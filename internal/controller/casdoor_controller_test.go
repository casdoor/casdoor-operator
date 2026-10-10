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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/casdoor/casdoor-operator/api/v1alpha1"
)

const (
	timeout  = 10 * time.Second
	interval = 100 * time.Millisecond
)

var _ = Describe("Casdoor controller", func() {
	var (
		ctx       context.Context
		namespace string
	)

	key := func(name string) client.ObjectKey {
		return client.ObjectKey{Namespace: namespace, Name: name}
	}

	getDeployment := func() *appsv1.Deployment {
		deployment := &appsv1.Deployment{}
		Eventually(func() error { return k8sClient.Get(ctx, key("casdoor"), deployment) }, timeout, interval).Should(Succeed())
		return deployment
	}

	secretHash := func() string {
		return getDeployment().Spec.Template.Annotations[secretHashAnnotation]
	}

	readyCondition := func() *metav1.Condition {
		cr := &operatorv1alpha1.Casdoor{}
		if err := k8sClient.Get(ctx, key("casdoor"), cr); err != nil {
			return nil
		}
		return meta.FindStatusCondition(cr.Status.Conditions, operatorv1alpha1.ConditionReady)
	}

	readyReason := func() string {
		if condition := readyCondition(); condition != nil {
			return condition.Reason
		}
		return ""
	}

	BeforeEach(func() {
		ctx = context.Background()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
	})

	Context("with sqlite", func() {
		var cr *operatorv1alpha1.Casdoor

		BeforeEach(func() {
			cr = &operatorv1alpha1.Casdoor{
				ObjectMeta: metav1.ObjectMeta{Name: "casdoor", Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorSpec{
					Database: operatorv1alpha1.DatabaseSpec{
						Driver:  operatorv1alpha1.DriverSQLite,
						Storage: &operatorv1alpha1.StorageSpec{Size: resource.MustParse("1Gi")},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		})

		It("creates the deployment, the service and a data volume it does not own", func() {
			deployment := getDeployment()
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(testImage))
			Expect(metav1.IsControlledBy(deployment, cr)).To(BeTrue())

			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, key("casdoor"), service)).To(Succeed())
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8000)))

			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, key("casdoor-data"), pvc)).To(Succeed())
			Expect(pvc.OwnerReferences).To(BeEmpty())

			Eventually(readyReason, timeout, interval).Should(Equal(reasonRollingOut))
		})

		It("reports Ready once the deployment is available", func() {
			deployment := getDeployment()
			deployment.Status = appsv1.DeploymentStatus{
				ObservedGeneration: deployment.Generation,
				Replicas:           1,
				UpdatedReplicas:    1,
				ReadyReplicas:      1,
				AvailableReplicas:  1,
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			Eventually(readyReason, timeout, interval).Should(Equal(reasonAvailable))
			updated := &operatorv1alpha1.Casdoor{}
			Expect(k8sClient.Get(ctx, key("casdoor"), updated)).To(Succeed())
			Expect(updated.Status.ReadyReplicas).To(Equal(int32(1)))
			Expect(updated.Status.Image).To(Equal(testImage))
			Expect(updated.Status.URL).To(Equal("http://casdoor." + namespace + ".svc:8000"))
			Expect(updated.Status.Selector).To(Equal("app.kubernetes.io/instance=casdoor,app.kubernetes.io/name=casdoor"))
		})

		It("does not rewrite the deployment when nothing changed", func() {
			resourceVersion := getDeployment().ResourceVersion
			Consistently(func() string { return getDeployment().ResourceVersion }, 2*time.Second, 200*time.Millisecond).
				Should(Equal(resourceVersion))
		})

		It("refuses more than one replica", func() {
			patch := client.MergeFrom(cr.DeepCopy())
			cr.Spec.Replicas = ptr.To(int32(2))
			cr.Spec.Redis = &operatorv1alpha1.RedisSpec{Endpoint: "redis:6379"}
			err := k8sClient.Patch(ctx, cr, patch)
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("sqlite supports a single replica only"))
		})
	})

	Context("with postgres", func() {
		var cr *operatorv1alpha1.Casdoor

		BeforeEach(func() {
			cr = &operatorv1alpha1.Casdoor{
				ObjectMeta: metav1.ObjectMeta{Name: "casdoor", Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorSpec{
					Replicas: ptr.To(int32(3)),
					Database: operatorv1alpha1.DatabaseSpec{
						Driver:            operatorv1alpha1.DriverPostgres,
						Host:              "postgres",
						User:              "casdoor",
						PasswordSecretRef: secretRef("credentials", "db-password"),
					},
					Redis:                      &operatorv1alpha1.RedisSpec{Endpoint: "redis:6379"},
					InitAdminPasswordSecretRef: secretRef("credentials", "admin-password"),
					Ingress: &operatorv1alpha1.IngressSpec{
						Host:          "door.example.com",
						TLSSecretName: "door-tls",
					},
				},
			}
		})

		createSecret := func() *corev1.Secret {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: namespace},
				StringData: map[string]string{"db-password": "one", "admin-password": "two"},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			return secret
		}

		updateSecret := func(secret *corev1.Secret, data map[string]string) {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			secret.StringData = data
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())
		}

		It("waits for the secret and continues as soon as it appears", func() {
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(readyReason, timeout, interval).Should(Equal(reasonSecretError))
			Expect(readyCondition().Message).To(ContainSubstring(`secret "credentials" not found`))
			Expect(k8sClient.Get(ctx, key("casdoor"), &appsv1.Deployment{})).NotTo(Succeed())

			createSecret()
			deployment := getDeployment()
			Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
			env := envMap(deployment.Spec.Template.Spec.Containers[0].Env)
			Expect(env["origin"].Value).To(Equal("https://door.example.com"))
			Expect(env["CASDOOR_DB_PASSWORD"].ValueFrom.SecretKeyRef.Key).To(Equal("db-password"))
		})

		It("rolls the pods when a password the process uses changes", func() {
			secret := createSecret()
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(secretHash, timeout, interval).ShouldNot(BeEmpty())
			hash := secretHash()

			updateSecret(secret, map[string]string{"admin-password": "changed"})
			Consistently(secretHash, time.Second, 200*time.Millisecond).Should(Equal(hash))

			updateSecret(secret, map[string]string{"db-password": "changed"})
			Eventually(secretHash, timeout, interval).ShouldNot(Equal(hash))
		})

		It("creates and removes the ingress and the disruption budget", func() {
			createSecret()
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			getDeployment()

			ingress := &networkingv1.Ingress{}
			Eventually(func() error { return k8sClient.Get(ctx, key("casdoor"), ingress) }, timeout, interval).Should(Succeed())
			Expect(ingress.Spec.TLS[0].SecretName).To(Equal("door-tls"))
			Expect(k8sClient.Get(ctx, key("casdoor"), &policyv1.PodDisruptionBudget{})).To(Succeed())

			patch := client.MergeFrom(cr.DeepCopy())
			cr.Spec.Ingress = nil
			cr.Spec.Origin = "https://id.example.org"
			cr.Spec.Replicas = ptr.To(int32(1))
			Expect(k8sClient.Patch(ctx, cr, patch)).To(Succeed())

			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, key("casdoor"), &networkingv1.Ingress{}))
			}, timeout, interval).Should(BeTrue())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, key("casdoor"), &policyv1.PodDisruptionBudget{}))
			}, timeout, interval).Should(BeTrue())
			Eventually(func() string {
				return envValue(getDeployment().Spec.Template.Spec.Containers[0].Env, "origin")
			}, timeout, interval).Should(Equal("https://id.example.org"))
		})

		It("refuses several replicas without redis", func() {
			cr.Spec.Redis = nil
			err := k8sClient.Create(ctx, cr)
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("redis is required"))
		})

		It("refuses a database without a host", func() {
			cr.Spec.Database.Host = ""
			err := k8sClient.Create(ctx, cr)
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("host or dataSourceNameSecretRef is required"))
		})
	})
})
