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

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/casdoor/casdoor-operator/api/v1alpha1"
	"github.com/casdoor/casdoor-operator/internal/casdoor"
)

// CasdoorOrganizationReconciler keeps an organization in Casdoor in line with a CasdoorOrganization.
type CasdoorOrganizationReconciler struct {
	client.Client
	Recorder    record.EventRecorder
	APIEndpoint APIEndpointFunc
}

// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoororganizations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoororganizations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoororganizations/finalizers,verbs=update

func organizationName(org *operatorv1alpha1.CasdoorOrganization) string {
	if org.Spec.Name != "" {
		return org.Spec.Name
	}
	return org.Name
}

func (r *CasdoorOrganizationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	org := &operatorv1alpha1.CasdoorOrganization{}
	if err := r.Get(ctx, req.NamespacedName, org); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	name := organizationName(org)
	id := "admin/" + name

	if !org.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(org, objectFinalizer) {
			return ctrl.Result{}, nil
		}
		if org.Spec.DeletionPolicy == operatorv1alpha1.DeletionPolicyDelete {
			wasSynced := meta.IsStatusConditionTrue(org.Status.Conditions, operatorv1alpha1.ConditionReady)
			if err := deleteObject(ctx, r.Client, org.Namespace, org.Spec.CasdoorRef, r.APIEndpoint, wasSynced, casdoor.KindOrganization, id); err != nil {
				return ctrl.Result{}, err
			}
		}
		patch := client.MergeFrom(org.DeepCopy())
		controllerutil.RemoveFinalizer(org, objectFinalizer)
		return ctrl.Result{}, r.Patch(ctx, org, patch)
	}

	if !controllerutil.ContainsFinalizer(org, objectFinalizer) {
		patch := client.MergeFrom(org.DeepCopy())
		controllerutil.AddFinalizer(org, objectFinalizer)
		if err := r.Patch(ctx, org, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	conn, err := connect(ctx, r.Client, org.Namespace, org.Spec.CasdoorRef, r.APIEndpoint)
	if errors.Is(err, errCasdoorNotReady) || errors.Is(err, errCasdoorGone) {
		return ctrl.Result{RequeueAfter: waitPeriod}, r.setStatus(ctx, org, metav1.ConditionFalse, reasonWaiting, err.Error())
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	desired, err := desiredFields(org.Spec, org.Spec.ExtraFields)
	if err != nil {
		return ctrl.Result{}, r.setStatus(ctx, org, metav1.ConditionFalse, reasonSyncFailed, err.Error())
	}
	desired["owner"] = "admin"
	desired["name"] = name

	if _, err = syncObject(ctx, conn, casdoor.KindOrganization, id, desired); err != nil {
		r.Recorder.Event(org, corev1.EventTypeWarning, reasonSyncFailed, err.Error())
		_ = r.setStatus(ctx, org, metav1.ConditionFalse, reasonSyncFailed, err.Error())
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: resyncPeriod}, r.setStatus(ctx, org, metav1.ConditionTrue, reasonSynced, "organization "+name+" is in sync")
}

func (r *CasdoorOrganizationReconciler) setStatus(ctx context.Context, org *operatorv1alpha1.CasdoorOrganization, status metav1.ConditionStatus, reason string, message string) error {
	updated := org.Status.DeepCopy()
	updated.ObservedGeneration = org.Generation
	meta.SetStatusCondition(&updated.Conditions, metav1.Condition{
		Type:               operatorv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: org.Generation,
	})
	if apiequality.Semantic.DeepEqual(&org.Status, updated) {
		return nil
	}
	patch := client.MergeFrom(org.DeepCopy())
	org.Status = *updated
	return r.Status().Patch(ctx, org, patch)
}

func (r *CasdoorOrganizationReconciler) organizationsForCasdoor(ctx context.Context, obj client.Object) []reconcile.Request {
	list := &operatorv1alpha1.CasdoorOrganizationList{}
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

func (r *CasdoorOrganizationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.CasdoorOrganization{}).
		Watches(&operatorv1alpha1.Casdoor{}, handler.EnqueueRequestsFromMapFunc(r.organizationsForCasdoor)).
		Complete(r)
}
