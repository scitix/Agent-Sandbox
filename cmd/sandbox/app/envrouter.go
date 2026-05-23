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

package controller

import (
	"context"
	"fmt"

	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envscheduler"
)

// envGetterFromCache adapts a controller-runtime client.Client to the
// envscheduler.EnvGetter interface. The underlying client reads through the
// shared cache, so each GetEnv is a cheap in-memory lookup.
type envGetterFromCache struct {
	client client.Client
}

func (g *envGetterFromCache) GetEnv(ns, name string) (*agentsv1alpha1.SandboxEnv, bool) {
	env := &agentsv1alpha1.SandboxEnv{}
	if err := g.client.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: name}, env); err != nil {
		return nil, false
	}
	return env, true
}

// setupEnvRouterInformer registers an event handler on the SandboxEnv
// informer so the router's cached route table stays in sync with K8s state.
// Add/Update reload the entry; Delete drops it. Errors that surface here are
// almost always programming bugs (manager already started, type unknown);
// fatal at startup.
func setupEnvRouterInformer(mgr ctrl.Manager, router *envscheduler.Manager) error {
	informer, err := mgr.GetCache().GetInformer(context.Background(), &agentsv1alpha1.SandboxEnv{})
	if err != nil {
		return fmt.Errorf("get SandboxEnv informer: %w", err)
	}
	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if env, ok := obj.(*agentsv1alpha1.SandboxEnv); ok {
				router.OnEnvUpsert(env)
			}
		},
		UpdateFunc: func(_, obj any) {
			if env, ok := obj.(*agentsv1alpha1.SandboxEnv); ok {
				router.OnEnvUpsert(env)
			}
		},
		DeleteFunc: func(obj any) {
			env, ok := obj.(*agentsv1alpha1.SandboxEnv)
			if !ok {
				// DeletionFinalStateUnknown — extract the wrapped object.
				if tomb, isTomb := obj.(cache.DeletedFinalStateUnknown); isTomb {
					env, ok = tomb.Obj.(*agentsv1alpha1.SandboxEnv)
				}
			}
			if !ok {
				return
			}
			router.OnEnvDelete(client.ObjectKeyFromObject(env))
		},
	})
	return err
}
