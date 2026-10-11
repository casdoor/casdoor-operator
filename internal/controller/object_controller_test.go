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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/casdoor/casdoor-operator/api/v1alpha1"
	"github.com/casdoor/casdoor-operator/internal/casdoor"
)

var _ = Describe("Casdoor objects", func() {
	var (
		ctx       context.Context
		namespace string
	)

	key := func(name string) client.ObjectKey {
		return client.ObjectKey{Namespace: namespace, Name: name}
	}

	condition := func(obj client.Object, conditionType string) func() *metav1.Condition {
		return func() *metav1.Condition {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
				return nil
			}
			var conditions []metav1.Condition
			switch typed := obj.(type) {
			case *operatorv1alpha1.Casdoor:
				conditions = typed.Status.Conditions
			case *operatorv1alpha1.CasdoorOrganization:
				conditions = typed.Status.Conditions
			case *operatorv1alpha1.CasdoorApplication:
				conditions = typed.Status.Conditions
			}
			return meta.FindStatusCondition(conditions, conditionType)
		}
	}

	reason := func(obj client.Object, conditionType string) func() string {
		return func() string {
			if found := condition(obj, conditionType)(); found != nil {
				return found.Reason
			}
			return ""
		}
	}

	createSecret := func(name string, data map[string]string) {
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			StringData: data,
		})).To(Succeed())
	}

	markDeploymentAvailable := func(name string) {
		deployment := &appsv1.Deployment{}
		Eventually(func() error { return k8sClient.Get(ctx, key(name), deployment) }, timeout, interval).Should(Succeed())
		deployment.Status = appsv1.DeploymentStatus{
			ObservedGeneration: deployment.Generation,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
	}

	BeforeEach(func() {
		ctx = context.Background()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "objects-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
	})

	Context("API credentials of a managed instance", func() {
		newCasdoorWithAdminPassword := func(password string) *operatorv1alpha1.Casdoor {
			createSecret("admin", map[string]string{"password": password})
			cr := &operatorv1alpha1.Casdoor{
				ObjectMeta: metav1.ObjectMeta{Name: "casdoor", Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorSpec{
					Database:                   operatorv1alpha1.DatabaseSpec{Driver: operatorv1alpha1.DriverSQLite},
					InitAdminPasswordSecretRef: secretRef("admin", "password"),
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			return cr
		}

		It("signs in once Casdoor is ready and stores the credentials of app-built-in", func() {
			cr := newCasdoorWithAdminPassword(fakeAdminPassword)
			Eventually(reason(cr, operatorv1alpha1.ConditionAPIReady), timeout, interval).Should(Equal(reasonWaiting))

			markDeploymentAvailable("casdoor")
			Eventually(reason(cr, operatorv1alpha1.ConditionAPIReady), timeout, interval).Should(Equal(reasonAvailable))
			Expect(cr.Status.APICredentialsSecretName).To(Equal("casdoor-api-credentials"))

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, key("casdoor-api-credentials"), secret)).To(Succeed())
			Expect(string(secret.Data["clientId"])).To(Equal(fakeClientId))
			Expect(string(secret.Data["clientSecret"])).To(Equal(fakeClientSecret))
			Expect(metav1.IsControlledBy(secret, cr)).To(BeTrue())
		})

		It("reports a wrong admin password", func() {
			cr := newCasdoorWithAdminPassword("wrong")
			markDeploymentAvailable("casdoor")
			Eventually(reason(cr, operatorv1alpha1.ConditionAPIReady), timeout, interval).Should(Equal("BootstrapFailed"))
			Expect(condition(cr, operatorv1alpha1.ConditionAPIReady)().Message).To(ContainSubstring("password or code is incorrect"))
			Expect(cr.Status.APICredentialsSecretName).To(BeEmpty())
		})

		It("asks for credentials when there is no admin password", func() {
			cr := &operatorv1alpha1.Casdoor{
				ObjectMeta: metav1.ObjectMeta{Name: "casdoor", Namespace: namespace},
				Spec:       operatorv1alpha1.CasdoorSpec{Database: operatorv1alpha1.DatabaseSpec{Driver: operatorv1alpha1.DriverSQLite}},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(reason(cr, operatorv1alpha1.ConditionAPIReady), timeout, interval).Should(Equal("NoCredentials"))
		})
	})

	Context("organizations", func() {
		var orgName string

		BeforeEach(func() {
			orgName = "org-" + namespace
			createSecret("api", map[string]string{"clientId": fakeClientId, "clientSecret": fakeClientSecret})
			Expect(k8sClient.Create(ctx, &operatorv1alpha1.Casdoor{
				ObjectMeta: metav1.ObjectMeta{Name: "casdoor", Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorSpec{
					Database:                operatorv1alpha1.DatabaseSpec{Driver: operatorv1alpha1.DriverSQLite},
					APICredentialsSecretRef: &corev1.LocalObjectReference{Name: "api"},
				},
			})).To(Succeed())
		})

		newOrganization := func(policy string) *operatorv1alpha1.CasdoorOrganization {
			org := &operatorv1alpha1.CasdoorOrganization{
				ObjectMeta: metav1.ObjectMeta{Name: orgName, Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorOrganizationSpec{
					CasdoorRef:     operatorv1alpha1.CasdoorReference{Name: "casdoor"},
					DeletionPolicy: policy,
					DisplayName:    "ACME",
					PasswordType:   "bcrypt",
					Languages:      []string{"en", "zh"},
					ExtraFields:    map[string]apiextensionsv1.JSON{"initScore": {Raw: []byte("100")}},
				},
			}
			Expect(k8sClient.Create(ctx, org)).To(Succeed())
			Eventually(reason(org, operatorv1alpha1.ConditionReady), timeout, interval).Should(Equal(reasonSynced))
			return org
		}

		It("creates the organization with the typed and the extra fields", func() {
			newOrganization("")
			obj := fake.get(casdoor.KindOrganization, "admin/"+orgName)
			Expect(obj).To(HaveKeyWithValue("owner", "admin"))
			Expect(obj).To(HaveKeyWithValue("displayName", "ACME"))
			Expect(obj).To(HaveKeyWithValue("passwordType", "bcrypt"))
			Expect(obj["languages"]).To(ConsistOf("en", "zh"))
			Expect(obj["initScore"]).To(BeNumerically("==", 100))
			Expect(obj).NotTo(HaveKey("casdoorRef"))
			Expect(obj).NotTo(HaveKey("deletionPolicy"))
		})

		It("writes spec changes and keeps fields it does not manage", func() {
			org := newOrganization("")
			obj := fake.get(casdoor.KindOrganization, "admin/"+orgName)
			obj["favicon"] = "https://acme.example.com/favicon.ico"
			obj["displayName"] = "changed in the console"
			fake.put(casdoor.KindOrganization, obj)

			patch := client.MergeFrom(org.DeepCopy())
			org.Spec.Tags = []string{"vip"}
			Expect(k8sClient.Patch(ctx, org, patch)).To(Succeed())

			Eventually(func() interface{} {
				return fake.get(casdoor.KindOrganization, "admin/"+orgName)["tags"]
			}, timeout, interval).Should(ConsistOf("vip"))
			obj = fake.get(casdoor.KindOrganization, "admin/"+orgName)
			Expect(obj).To(HaveKeyWithValue("displayName", "ACME"))
			Expect(obj).To(HaveKeyWithValue("favicon", "https://acme.example.com/favicon.ico"))
		})

		It("keeps the organization in Casdoor by default when deleted", func() {
			org := newOrganization("")
			Expect(k8sClient.Delete(ctx, org)).To(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, key(orgName), org))
			}, timeout, interval).Should(BeTrue())
			Expect(fake.get(casdoor.KindOrganization, "admin/"+orgName)).NotTo(BeNil())
		})

		It("deletes the organization in Casdoor with the Delete policy", func() {
			org := newOrganization(operatorv1alpha1.DeletionPolicyDelete)
			Expect(k8sClient.Delete(ctx, org)).To(Succeed())
			Eventually(func() casdoor.Object {
				return fake.get(casdoor.KindOrganization, "admin/"+orgName)
			}, timeout, interval).Should(BeNil())
		})

		It("waits while the Casdoor object does not exist and can still be deleted", func() {
			org := &operatorv1alpha1.CasdoorOrganization{
				ObjectMeta: metav1.ObjectMeta{Name: orgName, Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorOrganizationSpec{
					CasdoorRef: operatorv1alpha1.CasdoorReference{Name: "missing"},
				},
			}
			Expect(k8sClient.Create(ctx, org)).To(Succeed())
			Eventually(reason(org, operatorv1alpha1.ConditionReady), timeout, interval).Should(Equal(reasonWaiting))

			Expect(k8sClient.Delete(ctx, org)).To(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, key(orgName), org))
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("applications on an external Casdoor", func() {
		var appName string

		external := func(secret string) operatorv1alpha1.CasdoorReference {
			return operatorv1alpha1.CasdoorReference{External: &operatorv1alpha1.ExternalCasdoor{
				Endpoint:             fake.server.URL,
				CredentialsSecretRef: corev1.LocalObjectReference{Name: secret},
			}}
		}

		BeforeEach(func() {
			appName = "app-" + namespace
			createSecret("external", map[string]string{"clientId": fakeClientId, "clientSecret": fakeClientSecret})
		})

		newApplication := func(ref operatorv1alpha1.CasdoorReference) *operatorv1alpha1.CasdoorApplication {
			app := &operatorv1alpha1.CasdoorApplication{
				ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorApplicationSpec{
					CasdoorRef:    ref,
					Organization:  "acme",
					RedirectUris:  []string{"https://portal.example.com/callback"},
					ExpireInHours: ptr.To(int32(24)),
					Cert:          "cert-built-in",
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())
			return app
		}

		It("creates the application and publishes its credentials", func() {
			app := newApplication(external("external"))
			Eventually(reason(app, operatorv1alpha1.ConditionReady), timeout, interval).Should(Equal(reasonSynced))
			Expect(app.Status.ClientId).To(Equal("generated-" + appName))

			obj := fake.get(casdoor.KindApplication, "admin/"+appName)
			Expect(obj).To(HaveKeyWithValue("organization", "acme"))
			Expect(obj["expireInHours"]).To(BeNumerically("==", 24))

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, key(appName+"-casdoor"), secret)).To(Succeed())
			Expect(metav1.IsControlledBy(secret, app)).To(BeTrue())
			Expect(secret.Data).To(HaveKeyWithValue("endpoint", []byte(fake.server.URL)))
			Expect(secret.Data).To(HaveKeyWithValue("clientId", []byte("generated-"+appName)))
			Expect(secret.Data).To(HaveKeyWithValue("clientSecret", []byte("generated-secret")))
			Expect(secret.Data).To(HaveKeyWithValue("organizationName", []byte("acme")))
			Expect(secret.Data).To(HaveKeyWithValue("applicationName", []byte(appName)))
			Expect(secret.Data).To(HaveKeyWithValue("certificate", []byte(fakeCertificate)))
		})

		It("sets the client secret from a Secret and deletes the application with the object", func() {
			createSecret("client", map[string]string{"secret": "chosen-secret"})
			app := &operatorv1alpha1.CasdoorApplication{
				ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
				Spec: operatorv1alpha1.CasdoorApplicationSpec{
					CasdoorRef:            external("external"),
					Organization:          "acme",
					ClientId:              "chosen-id",
					ClientSecretSecretRef: secretRef("client", "secret"),
					CredentialsSecretName: "portal-oidc",
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())
			Eventually(reason(app, operatorv1alpha1.ConditionReady), timeout, interval).Should(Equal(reasonSynced))

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, key("portal-oidc"), secret)).To(Succeed())
			Expect(secret.Data).To(HaveKeyWithValue("clientId", []byte("chosen-id")))
			Expect(secret.Data).To(HaveKeyWithValue("clientSecret", []byte("chosen-secret")))
			Expect(secret.Data).To(HaveKeyWithValue("certificate", []byte("")))

			Expect(k8sClient.Delete(ctx, app)).To(Succeed())
			Eventually(func() casdoor.Object {
				return fake.get(casdoor.KindApplication, "admin/"+appName)
			}, timeout, interval).Should(BeNil())
		})

		It("reports rejected credentials", func() {
			createSecret("wrong", map[string]string{"clientId": fakeClientId, "clientSecret": "wrong"})
			app := newApplication(external("wrong"))
			Eventually(reason(app, operatorv1alpha1.ConditionReady), timeout, interval).Should(Equal(reasonSyncFailed))
			Expect(condition(app, operatorv1alpha1.ConditionReady)().Message).To(ContainSubstring("Unauthorized operation"))
		})

		It("waits for a credentials Secret that does not exist yet", func() {
			app := newApplication(external("later"))
			Eventually(reason(app, operatorv1alpha1.ConditionReady), timeout, interval).Should(Equal(reasonWaiting))

			createSecret("later", map[string]string{"clientId": fakeClientId, "clientSecret": fakeClientSecret})
			Eventually(reason(app, operatorv1alpha1.ConditionReady), 2*waitPeriod, interval).Should(Equal(reasonSynced))
		})

		It("refuses a reference to both a managed and an external Casdoor", func() {
			ref := external("external")
			ref.Name = "casdoor"
			app := &operatorv1alpha1.CasdoorApplication{
				ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
				Spec:       operatorv1alpha1.CasdoorApplicationSpec{CasdoorRef: ref, Organization: "acme"},
			}
			err := k8sClient.Create(ctx, app)
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("exactly one of name and external must be set"))
		})
	})

	It("lets the user override the LDAP port", func() {
		cr := newCasdoor("sqlite")
		Expect(ldapPort(cr)).To(Equal(int32(10389)))
		cr.Spec.Config = map[string]string{"ldapServerPort": "1389"}
		Expect(ldapPort(cr)).To(Equal(int32(1389)))
		Expect(envValue(buildEnv(cr), "ldapServerPort")).To(Equal("1389"))
		Expect(buildService(cr).Spec.Ports[1].Port).To(Equal(int32(389)))
	})

})
