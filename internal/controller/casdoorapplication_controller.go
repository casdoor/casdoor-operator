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
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/casdoor/casdoor-operator/api/v1alpha1"
	"github.com/casdoor/casdoor-operator/internal/casdoor"
)

// CasdoorApplicationReconciler keeps an application in Casdoor in line with a CasdoorApplication,
// and publishes its client credentials in a Secret.
type CasdoorApplicationReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	APIEndpoint APIEndpointFunc
}

// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoorapplications,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoorapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoorapplications/finalizers,verbs=update

func applicationName(app *operatorv1alpha1.CasdoorApplication) string {
	if app.Spec.Name != "" {
		return app.Spec.Name
	}
	return app.Name
}

func credentialsSecretName(app *operatorv1alpha1.CasdoorApplication) string {
	if app.Spec.CredentialsSecretName != "" {
		return app.Spec.CredentialsSecretName
	}
	return app.Name + "-casdoor"
}

func (r *CasdoorApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	app := &operatorv1alpha1.CasdoorApplication{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	name := applicationName(app)
	id := "admin/" + name

	if !app.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(app, objectFinalizer) {
			return ctrl.Result{}, nil
		}
		if app.Spec.DeletionPolicy != operatorv1alpha1.DeletionPolicyRetain {
			wasSynced := meta.IsStatusConditionTrue(app.Status.Conditions, operatorv1alpha1.ConditionReady)
			if err := deleteObject(ctx, r.Client, app.Namespace, app.Spec.CasdoorRef, r.APIEndpoint, wasSynced, casdoor.KindApplication, id); err != nil {
				return ctrl.Result{}, err
			}
		}
		patch := client.MergeFrom(app.DeepCopy())
		controllerutil.RemoveFinalizer(app, objectFinalizer)
		return ctrl.Result{}, r.Patch(ctx, app, patch)
	}

	if !controllerutil.ContainsFinalizer(app, objectFinalizer) {
		patch := client.MergeFrom(app.DeepCopy())
		controllerutil.AddFinalizer(app, objectFinalizer)
		if err := r.Patch(ctx, app, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	conn, err := connect(ctx, r.Client, app.Namespace, app.Spec.CasdoorRef, r.APIEndpoint)
	if errors.Is(err, errCasdoorNotReady) || errors.Is(err, errCasdoorGone) {
		return ctrl.Result{RequeueAfter: waitPeriod}, r.setStatus(ctx, app, metav1.ConditionFalse, reasonWaiting, err.Error())
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	desired, err := r.desired(ctx, app, name)
	if err != nil {
		return ctrl.Result{RequeueAfter: waitPeriod}, r.setStatus(ctx, app, metav1.ConditionFalse, reasonSyncFailed, err.Error())
	}

	current, err := syncObject(ctx, conn, casdoor.KindApplication, id, desired)
	if err == nil {
		err = r.applyCredentialsSecret(ctx, app, conn, name, current)
	}
	if err != nil {
		r.Recorder.Event(app, corev1.EventTypeWarning, reasonSyncFailed, err.Error())
		_ = r.setStatus(ctx, app, metav1.ConditionFalse, reasonSyncFailed, err.Error())
		return ctrl.Result{}, err
	}

	app.Status.ClientId, _ = current["clientId"].(string)
	app.Status.CredentialsSecretName = credentialsSecretName(app)
	return ctrl.Result{RequeueAfter: resyncPeriod}, r.setStatus(ctx, app, metav1.ConditionTrue, reasonSynced, "application "+name+" is in sync")
}

func (r *CasdoorApplicationReconciler) desired(ctx context.Context, app *operatorv1alpha1.CasdoorApplication, name string) (casdoor.Object, error) {
	desired, err := desiredFields(app.Spec, app.Spec.ExtraFields, "clientSecretSecretRef", "credentialsSecretName")
	if err != nil {
		return nil, err
	}
	desired["owner"] = "admin"
	desired["name"] = name

	if ref := app.Spec.ClientSecretSecretRef; ref != nil {
		secret := &corev1.Secret{}
		if err = r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: ref.Name}, secret); err != nil {
			return nil, fmt.Errorf("read client secret: %w", err)
		}
		value := string(secret.Data[ref.Key])
		if value == "" {
			return nil, fmt.Errorf("secret %q has no key %q", ref.Name, ref.Key)
		}
		desired["clientSecret"] = value
	}
	return desired, nil
}

func (r *CasdoorApplicationReconciler) applyCredentialsSecret(ctx context.Context, app *operatorv1alpha1.CasdoorApplication, conn *connection, name string, current casdoor.Object) error {
	clientId, _ := current["clientId"].(string)
	clientSecret, _ := current["clientSecret"].(string)
	if clientId == "" || clientSecret == "" || clientSecret == "***" {
		return fmt.Errorf("application %s has no readable client credentials", name)
	}

	certificate := ""
	if cert, _ := current["cert"].(string); cert != "" {
		var err error
		if certificate, err = conn.client.GetCertificate(ctx, "admin/"+cert); err != nil {
			return err
		}
	}

	organization, _ := current["organization"].(string)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecretName(app),
			Namespace: app.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "casdoor-operator"},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"endpoint":         conn.endpoint,
			"clientId":         clientId,
			"clientSecret":     clientSecret,
			"organizationName": organization,
			"applicationName":  name,
			"certificate":      certificate,
		},
	}
	gvk, err := apiutil.GVKForObject(secret, r.Scheme)
	if err != nil {
		return err
	}
	secret.GetObjectKind().SetGroupVersionKind(gvk)
	if err = controllerutil.SetControllerReference(app, secret, r.Scheme); err != nil {
		return err
	}
	return r.Patch(ctx, secret, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership)
}

func (r *CasdoorApplicationReconciler) setStatus(ctx context.Context, app *operatorv1alpha1.CasdoorApplication, status metav1.ConditionStatus, reason string, message string) error {
	original := &operatorv1alpha1.CasdoorApplication{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(app), original); err != nil {
		return client.IgnoreNotFound(err)
	}
	updated := app.Status.DeepCopy()
	updated.ObservedGeneration = app.Generation
	meta.SetStatusCondition(&updated.Conditions, metav1.Condition{
		Type:               operatorv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: app.Generation,
	})
	if apiequality.Semantic.DeepEqual(&original.Status, updated) {
		return nil
	}
	patch := client.MergeFrom(original.DeepCopy())
	original.Status = *updated
	return r.Status().Patch(ctx, original, patch)
}

func (r *CasdoorApplicationReconciler) applicationsForCasdoor(ctx context.Context, obj client.Object) []reconcile.Request {
	list := &operatorv1alpha1.CasdoorApplicationList{}
	if err := r.List(ctx, list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		if list.Items[i].Spec.CasdoorRef.Name == obj.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return requests
}

func (r *CasdoorApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.CasdoorApplication{}).
		Owns(&corev1.Secret{}).
		Watches(&operatorv1alpha1.Casdoor{}, handler.EnqueueRequestsFromMapFunc(r.applicationsForCasdoor)).
		Complete(r)
}
