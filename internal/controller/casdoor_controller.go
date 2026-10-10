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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
)

const (
	fieldOwner         = "casdoor-operator"
	secretRetryPeriod  = 30 * time.Second
	reasonSecretError  = "SecretNotReady"
	reasonApplyError   = "ApplyFailed"
	reasonRollingOut   = "RollingOut"
	reasonScaledToZero = "ScaledToZero"
	reasonAvailable    = "Available"
)

var errSecretNotReady = errors.New("secret not ready")

// CasdoorReconciler reconciles a Casdoor object.
type CasdoorReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	DefaultImage string
}

// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.casdoor.org,resources=casdoors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func (r *CasdoorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cr := &operatorv1alpha1.Casdoor{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !cr.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	secretHash, err := r.hashSecrets(ctx, cr)
	if err != nil {
		if errors.Is(err, errSecretNotReady) {
			r.Recorder.Event(cr, corev1.EventTypeWarning, reasonSecretError, err.Error())
			return ctrl.Result{RequeueAfter: secretRetryPeriod}, r.setNotReady(ctx, cr, reasonSecretError, err.Error())
		}
		return ctrl.Result{}, err
	}

	deployment := buildDeployment(cr, r.DefaultImage, secretHash)
	if err := r.applyAll(ctx, cr, deployment); err != nil {
		r.Recorder.Event(cr, corev1.EventTypeWarning, reasonApplyError, err.Error())
		_ = r.setNotReady(ctx, cr, reasonApplyError, err.Error())
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.updateStatus(ctx, cr, deployment)
}

func (r *CasdoorReconciler) applyAll(ctx context.Context, cr *operatorv1alpha1.Casdoor, deployment *appsv1.Deployment) error {
	if pvc := buildDataPVC(cr); pvc != nil {
		if err := r.createIfAbsent(ctx, pvc); err != nil {
			return err
		}
	}

	if err := r.apply(ctx, cr, buildService(cr)); err != nil {
		return err
	}
	if err := r.apply(ctx, cr, deployment); err != nil {
		return err
	}

	if ingress := buildIngress(cr); ingress != nil {
		if err := r.apply(ctx, cr, ingress); err != nil {
			return err
		}
	} else if err := r.deleteOwned(ctx, cr, &networkingv1.Ingress{}); err != nil {
		return err
	}

	if pdb := buildPodDisruptionBudget(cr); pdb != nil {
		if err := r.apply(ctx, cr, pdb); err != nil {
			return err
		}
	} else if err := r.deleteOwned(ctx, cr, &policyv1.PodDisruptionBudget{}); err != nil {
		return err
	}
	return nil
}

func (r *CasdoorReconciler) apply(ctx context.Context, cr *operatorv1alpha1.Casdoor, obj client.Object) error {
	gvk, err := apiutil.GVKForObject(obj, r.Scheme)
	if err != nil {
		return err
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)
	if err := controllerutil.SetControllerReference(cr, obj, r.Scheme); err != nil {
		return err
	}
	if err := r.Patch(ctx, obj, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("apply %s %s: %w", gvk.Kind, obj.GetName(), err)
	}
	return nil
}

// The data volume is not owned by the Casdoor object, so deleting the object keeps the database.
func (r *CasdoorReconciler) createIfAbsent(ctx context.Context, obj client.Object) error {
	err := r.Create(ctx, obj)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *CasdoorReconciler) deleteOwned(ctx context.Context, cr *operatorv1alpha1.Casdoor, obj client.Object) error {
	err := r.Get(ctx, client.ObjectKeyFromObject(cr), obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(obj, cr) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, obj))
}

// hashSecrets checks that every referenced secret key exists and returns a digest of the
// values the running process depends on, so that changing one of them rolls the pods.
func (r *CasdoorReconciler) hashSecrets(ctx context.Context, cr *operatorv1alpha1.Casdoor) (string, error) {
	digest := sha256.New()
	cache := map[string]*corev1.Secret{}
	for _, ref := range secretRefs(cr) {
		secret, ok := cache[ref.Name]
		if !ok {
			secret = &corev1.Secret{}
			err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: ref.Name}, secret)
			if apierrors.IsNotFound(err) {
				return "", fmt.Errorf("%w: secret %q not found", errSecretNotReady, ref.Name)
			}
			if err != nil {
				return "", err
			}
			cache[ref.Name] = secret
		}
		value, ok := secret.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("%w: secret %q has no key %q", errSecretNotReady, ref.Name, ref.Key)
		}
		if ref == cr.Spec.InitAdminPasswordSecretRef {
			continue
		}
		fmt.Fprintf(digest, "%s/%s=%d:", ref.Name, ref.Key, len(value))
		digest.Write(value)
	}
	return hex.EncodeToString(digest.Sum(nil))[:16], nil
}

func secretRefs(cr *operatorv1alpha1.Casdoor) []*corev1.SecretKeySelector {
	var refs []*corev1.SecretKeySelector
	add := func(ref *corev1.SecretKeySelector) {
		if ref != nil {
			refs = append(refs, ref)
		}
	}
	add(cr.Spec.Database.PasswordSecretRef)
	add(cr.Spec.Database.DataSourceNameSecretRef)
	if cr.Spec.Redis != nil {
		add(cr.Spec.Redis.PasswordSecretRef)
	}
	add(cr.Spec.InitAdminPasswordSecretRef)
	if cr.Spec.InitData != nil {
		add(&cr.Spec.InitData.SecretRef)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Key < refs[j].Key
	})
	return refs
}

func (r *CasdoorReconciler) setNotReady(ctx context.Context, cr *operatorv1alpha1.Casdoor, reason string, message string) error {
	status := cr.Status.DeepCopy()
	status.ObservedGeneration = cr.Generation
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               operatorv1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cr.Generation,
	})
	return r.writeStatus(ctx, cr, status)
}

// The deployment holds the server response of the apply, so its status is current.
func (r *CasdoorReconciler) updateStatus(ctx context.Context, cr *operatorv1alpha1.Casdoor, deployment *appsv1.Deployment) error {
	replicas := int32(1)
	if cr.Spec.Replicas != nil {
		replicas = *cr.Spec.Replicas
	}

	status := cr.Status.DeepCopy()
	status.ObservedGeneration = cr.Generation
	status.Replicas = deployment.Status.Replicas
	status.ReadyReplicas = deployment.Status.ReadyReplicas
	status.Selector = metav1.FormatLabelSelector(deployment.Spec.Selector)
	status.Image = deployment.Spec.Template.Spec.Containers[0].Image
	status.URL = instanceURL(cr)

	condition := metav1.Condition{
		Type:               operatorv1alpha1.ConditionReady,
		ObservedGeneration: cr.Generation,
	}
	rolledOut := deployment.Status.ObservedGeneration >= deployment.Generation &&
		deployment.Status.UpdatedReplicas == replicas &&
		deployment.Status.Replicas == replicas &&
		deployment.Status.AvailableReplicas == replicas
	switch {
	case replicas == 0:
		condition.Status = metav1.ConditionFalse
		condition.Reason = reasonScaledToZero
		condition.Message = "replicas is 0"
	case rolledOut:
		condition.Status = metav1.ConditionTrue
		condition.Reason = reasonAvailable
		condition.Message = fmt.Sprintf("%d/%d replicas are available", deployment.Status.AvailableReplicas, replicas)
	default:
		condition.Status = metav1.ConditionFalse
		condition.Reason = reasonRollingOut
		condition.Message = fmt.Sprintf("%d/%d replicas are available, %d updated",
			deployment.Status.AvailableReplicas, replicas, deployment.Status.UpdatedReplicas)
	}
	meta.SetStatusCondition(&status.Conditions, condition)

	return r.writeStatus(ctx, cr, status)
}

func (r *CasdoorReconciler) writeStatus(ctx context.Context, cr *operatorv1alpha1.Casdoor, status *operatorv1alpha1.CasdoorStatus) error {
	if apiequality.Semantic.DeepEqual(&cr.Status, status) {
		return nil
	}
	patch := client.MergeFrom(cr.DeepCopy())
	cr.Status = *status
	return r.Status().Patch(ctx, cr, patch)
}

func (r *CasdoorReconciler) casdoorsForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	list := &operatorv1alpha1.CasdoorList{}
	if err := r.List(ctx, list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		for _, ref := range secretRefs(&list.Items[i]) {
			if ref.Name == obj.GetName() {
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
				break
			}
		}
	}
	return requests
}

func (r *CasdoorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Casdoor{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.casdoorsForSecret)).
		Complete(r)
}
