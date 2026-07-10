// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !linux

package egressproxy

import (
	"errors"
	"net"
)

// getOrigDst is Linux-only (SO_ORIGINAL_DST from netfilter). The proxy only
// runs inside a Linux sandbox Pod; this stub exists so the package builds on
// other platforms for tooling/tests of the pure logic.
func getOrigDst(int) (net.IP, int, error) {
	return nil, 0, errors.New("SO_ORIGINAL_DST is only supported on linux")
}
