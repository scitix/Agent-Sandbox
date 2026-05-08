#!/usr/bin/env python3
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
Kubernetes Pod Container Timeline Query Tool

This script queries Prometheus / VictoriaMetrics for the timeline of a Pod's
container restarts, image changes, and container ID updates.

Usage Example:
  # Option 1: Passing the token via environment variable
  export PROMETHEUS_TOKEN="your_token_here"
  python3 sandbox_timeline.py \
      --url https://prometheus.example.io \
      --pod swe-rl-envd-kk7bk \
      --duration 1h \
      --step 1m

  # Option 2: Passing the token via command-line arguments
  python3 sandbox_timeline.py \
      --url https://prometheus.example.io \
      --token "xxx=" \
      --pod swe-rl-envd-kk7bk \
      --duration 1h \
      --step 1m
"""

import argparse
import requests
import time
import re
import os
import sys
from datetime import datetime, timedelta
from typing import List, Dict, Any


def parse_duration_to_seconds(duration_str: str) -> int:
    """Parse strings like '5m', '1h', '1d' into seconds."""
    match = re.match(r"^(\d+)([smhdw])$", duration_str.strip())
    if not match:
        raise ValueError(f"Invalid duration format: '{duration_str}'. Supported formats: 5m, 1h, 1d")
    val, unit = int(match.group(1)), match.group(2)
    multipliers = {
        's': 1,
        'm': 60,
        'h': 3600,
        'd': 86400,
        'w': 86400 * 7
    }
    return val * multipliers[unit]


def format_timedelta(seconds: int) -> str:
    """Format seconds into a human-readable duration."""
    if seconds < 0:
        return "0s"
    return str(timedelta(seconds=int(seconds)))


def truncate_string(text: str, max_len: int = 35) -> str:
    """Truncate a string from the left if it exceeds max_len (keeps the trailing tag)."""
    if len(text) <= max_len:
        return text
    return "..." + text[-(max_len - 3):]


def fetch_metrics(url: str, token: str, pod: str, duration: str, step: str) -> List[Dict[str, Any]]:
    """Fetch and return the raw metrics from the Prometheus/VictoriaMetrics API."""
    end_time = int(time.time())
    start_time = end_time - parse_duration_to_seconds(duration)
    
    # PromQL join query
    query = f"""
    kube_pod_container_status_restarts_total{{pod="{pod}"}}
    * on(pod, container, namespace) group_left(image, container_id, image_spec)
    kube_pod_container_info{{pod="{pod}"}}
    """
    
    headers = {"Authorization": f"Bearer {token}"} if token else {}
    params = {
        "query": query.strip(),
        "start": start_time,
        "end": end_time,
        "step": step
    }
    
    api_url = url.rstrip('/') + '/api/v1/query_range'
    print(f"Querying timeline data for Pod '{pod}' over the past {duration} from {url}...\n")
    
    try:
        response = requests.get(api_url, params=params, headers=headers)
        response.raise_for_status()
        data = response.json()
    except requests.exceptions.RequestException as e:
        print(f"Request failed: {e}")
        sys.exit(1)
    except ValueError:
        print("Error: Failed to parse JSON response from the server.")
        sys.exit(1)
        
    if data.get('status') != 'success':
        print(f"Query failed: {data}")
        sys.exit(1)
        
    return data.get('data', {}).get('result',[])


def process_timeline(results: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Flatten and group contiguous container states using a sliding window."""
    timeline =[]
    
    # 1. Flatten the multi-series matrix into a list of points
    for series in results:
        metric = series.get('metric', {})
        container = metric.get('container', 'unknown')
        container_id = metric.get('container_id', 'unknown')
        image = metric.get('image', 'unknown')
        image_spec = metric.get('image_spec', 'unknown')
        
        for val in series.get('values', []):
            timeline.append({
                'ts': float(val[0]),
                'container': container,
                'restart_count': int(val[1]),
                'container_id': container_id,
                'image': image,
                'image_spec': image_spec
            })
            
    # 2. Sort points by container and chronological order
    timeline.sort(key=lambda x: (x['container'], x['ts']))
    
    # 3. Merge contiguous points with identical properties into a single state
    states =[]
    current_state = None
    
    for pt in timeline:
        state_key = (pt['container'], pt['restart_count'], pt['container_id'], pt['image'], pt['image_spec'])
        
        if current_state is None:
            current_state = pt.copy()
            current_state['start_ts'] = pt['ts']
            current_state['end_ts'] = pt['ts']
            current_state['key'] = state_key
        elif current_state['key'] == state_key:
            # State hasn't changed, extend the end timestamp
            current_state['end_ts'] = pt['ts']
        else:
            # State changed, archive the old state and start a new one
            states.append(current_state)
            current_state = pt.copy()
            current_state['start_ts'] = pt['ts']
            current_state['end_ts'] = pt['ts']
            current_state['key'] = state_key
            
    if current_state is not None:
        states.append(current_state)
        
    return states


def print_table(states: List[Dict[str, Any]], step_seconds: int):
    """Format and print the timeline states as a table."""
    headers_fmt = "{:<12} | {:<19} | {:<19} | {:<10} | {:<8} | {:<12} | {:<35} | {:<35}"
    row_fmt     = "{:<12} | {:<19} | {:<19} | {:<10} | {:<8} | {:<12} | {:<35} | {:<35}"
    separator   = "-" * 170
    
    print(separator)
    print(headers_fmt.format(
        "Container", "Start Time", "End Time", "Duration", "Restarts", "Container ID", "Spec Image", "Status Image"
    ))
    print(separator)
    
    for s in states:
        start_str = datetime.fromtimestamp(s['start_ts']).strftime('%Y-%m-%d %H:%M:%S')
        end_str = datetime.fromtimestamp(s['end_ts']).strftime('%Y-%m-%d %H:%M:%S')
        
        # Add one step to the duration to account for the discrete sampling interval
        duration_sec = s['end_ts'] - s['start_ts'] + step_seconds
        duration_str = format_timedelta(duration_sec)
            
        # Extract short Container ID (similar to Docker's standard 12-char ID)
        raw_cid = s['container_id']
        short_cid = raw_cid.split('://')[-1][:12] if '://' in raw_cid else raw_cid[:12]
        
        # Format the row variables
        print(row_fmt.format(
            s['container'][:12],
            start_str,
            end_str,
            duration_str,
            s['restart_count'],
            short_cid,
            truncate_string(s['image_spec']),
            truncate_string(s['image'])
        ))
    print(separator)


def main():
    parser = argparse.ArgumentParser(description="Query the timeline of Pod container restarts, images, and container IDs.")
    parser.add_argument("--url", default=os.getenv("PROMETHEUS_URL"), help="Prometheus / VMUI URL (or set via PROMETHEUS_URL env var)")
    parser.add_argument("--token", default=os.getenv("PROMETHEUS_TOKEN"), help="Bearer Token (or set via PROMETHEUS_TOKEN env var)")
    parser.add_argument("--pod", required=True, help="Target Pod name (e.g., swe-rl-envd-kk7bk)")
    parser.add_argument("--duration", default="1h", help="Query duration, e.g., 5m, 1h, 1d (default: 1h)")
    parser.add_argument("--step", default="1m", help="Prometheus sampling step, e.g., 15s, 1m (default: 1m)")
    
    args = parser.parse_args()

    if not args.url:
        parser.error("You must provide the monitoring URL via the --url argument or PROMETHEUS_URL environment variable.")

    # Execute flow
    try:
        step_seconds = parse_duration_to_seconds(args.step)
        results = fetch_metrics(args.url, args.token, args.pod, args.duration, args.step)
        
        if not results:
            print("No data found. Please verify the Pod name and ensure it existed during the specified timeframe.")
            sys.exit(0)
            
        states = process_timeline(results)
        print_table(states, step_seconds)
        
    except ValueError as ve:
        print(f"Configuration Error: {ve}")
        sys.exit(1)


if __name__ == "__main__":
    main()