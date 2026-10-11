// Copyright 2026 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/casdoor/casdoor-operator/api/v1alpha1"
	"github.com/casdoor/casdoor-operator/internal/casdoor"
)

const (
	objectFinalizer = "operator.casdoor.org/finalizer"
	resyncPeriod    = 10 * time.Minute
	waitPeriod      = 15 * time.Second

	reasonWaiting    = "WaitingForCasdoor"
	reasonSyncFailed = "SyncFailed"
	reasonSynced     = "Synced"

	clientIdKey     = "clientId"
	clientSecretKey = "clientSecret"
)

var (
	errCasdoorNotReady = errors.New("casdoor is not ready")
	errCasdoorGone     = errors.New("casdoor is gone")
)

// APIEndpointFunc returns the URL the operator uses to reach a managed Casdoor instance.
type APIEndpointFunc func(cr *operatorv1alpha1.Casdoor) string

func apiEndpoint(override APIEndpointFunc, cr *operatorv1alpha1.Casdoor) string {
	if override != nil {
		return override(cr)
	}
	return serviceURL(cr)
}

type connection struct {
	client *casdoor.Client
	// endpoint is the address applications use, which may differ from the one the operator calls.
	endpoint string
}

func readCredentials(ctx context.Context, c client.Client, namespace string, name string) (string, string, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		return "", "", fmt.Errorf("%w: secret %q not found", errCasdoorNotReady, name)
	}
	if err != nil {
		return "", "", err
	}
	clientId, clientSecret := string(secret.Data[clientIdKey]), string(secret.Data[clientSecretKey])
	if clientId == "" || clientSecret == "" {
		return "", "", fmt.Errorf("%w: secret %q needs the %s and %s keys", errCasdoorNotReady, name, clientIdKey, clientSecretKey)
	}
	return clientId, clientSecret, nil
}

func connect(ctx context.Context, c client.Client, namespace string, ref operatorv1alpha1.CasdoorReference, override APIEndpointFunc) (*connection, error) {
	if ref.External != nil {
		clientId, clientSecret, err := readCredentials(ctx, c, namespace, ref.External.CredentialsSecretRef.Name)
		if err != nil {
			return nil, err
		}
		return &connection{
			client:   casdoor.NewClient(ref.External.Endpoint, clientId, clientSecret),
			endpoint: ref.External.Endpoint,
		}, nil
	}

	cr := &operatorv1alpha1.Casdoor{}
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, cr)
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("%w: Casdoor %q not found", errCasdoorGone, ref.Name)
	}
	if err != nil {
		return nil, err
	}
	if !cr.DeletionTimestamp.IsZero() {
		return nil, fmt.Errorf("%w: Casdoor %q is being deleted", errCasdoorGone, ref.Name)
	}
	if cr.Status.APICredentialsSecretName == "" {
		return nil, fmt.Errorf("%w: Casdoor %q has no API credentials yet", errCasdoorNotReady, ref.Name)
	}

	clientId, clientSecret, err := readCredentials(ctx, c, namespace, cr.Status.APICredentialsSecretName)
	if err != nil {
		return nil, err
	}
	return &connection{
		client:   casdoor.NewClient(apiEndpoint(override, cr), clientId, clientSecret),
		endpoint: instanceURL(cr),
	}, nil
}

// desiredFields turns a spec into Casdoor fields: extraFields first, then the typed fields
// that are set, without the operator's own fields.
func desiredFields(spec interface{}, extra map[string]apiextensionsv1.JSON, operatorFields ...string) (casdoor.Object, error) {
	desired := casdoor.Object{}
	for key, value := range extra {
		var decoded interface{}
		if err := json.Unmarshal(value.Raw, &decoded); err != nil {
			return nil, fmt.Errorf("extraFields.%s: %w", key, err)
		}
		desired[key] = decoded
	}

	payload, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	typed := casdoor.Object{}
	if err = json.Unmarshal(payload, &typed); err != nil {
		return nil, err
	}
	for _, key := range append(operatorFields, "casdoorRef", "name", "deletionPolicy", "extraFields") {
		delete(typed, key)
	}
	for key, value := range typed {
		desired[key] = value
	}
	return desired, nil
}

// syncObject creates the object or writes the desired fields over the current one, and
// returns the object as Casdoor stores it.
func syncObject(ctx context.Context, conn *connection, kind string, id string, desired casdoor.Object) (casdoor.Object, error) {
	current, err := conn.client.GetObject(ctx, kind, id)
	if err != nil {
		return nil, err
	}

	if current == nil {
		if err = conn.client.AddObject(ctx, kind, desired); err != nil {
			return nil, err
		}
	} else if casdoor.Merge(current, desired) {
		if err = conn.client.UpdateObject(ctx, kind, id, current); err != nil {
			return nil, err
		}
	} else {
		return current, nil
	}

	current, err = conn.client.GetObject(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("%s %s is missing after it was written", kind, id)
	}
	return current, nil
}

// deleteObject deletes the object from Casdoor. It gives up when the Casdoor instance is gone,
// or when it never got credentials and the object was therefore never created.
func deleteObject(ctx context.Context, c client.Client, namespace string, ref operatorv1alpha1.CasdoorReference, override APIEndpointFunc, wasSynced bool, kind string, id string) error {
	conn, err := connect(ctx, c, namespace, ref, override)
	if errors.Is(err, errCasdoorGone) || (errors.Is(err, errCasdoorNotReady) && !wasSynced) {
		return nil
	}
	if err != nil {
		return err
	}

	current, err := conn.client.GetObject(ctx, kind, id)
	if err != nil || current == nil {
		return err
	}
	return conn.client.DeleteObject(ctx, kind, current)
}
