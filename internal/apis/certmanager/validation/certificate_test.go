/*
Copyright 2020 The cert-manager Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/jetstack/cert-manager/internal/api/validation"
	internalcmapi "github.com/jetstack/cert-manager/internal/apis/certmanager"
	cmmeta "github.com/jetstack/cert-manager/internal/apis/meta"
	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	cmapiv1alpha2 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmapiv1alpha3 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha3"
	cmapiv1beta1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1beta1"
	"github.com/stretchr/testify/assert"
)

var (
	validIssuerRef = cmmeta.ObjectReference{
		Name: "name",
		Kind: "ClusterIssuer",
	}
	someAdmissionRequest = &admissionv1.AdmissionRequest{
		RequestKind: &metav1.GroupVersionKind{
			Group:   "test",
			Kind:    "test",
			Version: "test",
		},
	}
	maxSecretTemplateAnnotationsBytesLimit = 256 * (1 << 10) // 256 kB
)

func strPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}

func TestValidateCertificate(t *testing.T) {
	fldPath := field.NewPath("spec")
	scenarios := map[string]struct {
		cfg      *internalcmapi.Certificate
		a        *admissionv1.AdmissionRequest
		errs     []*field.Error
		warnings validation.WarningList
	}{
		"valid basic certificate": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
		},
		"valid with blank issuerRef kind": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef: cmmeta.ObjectReference{
						Name: "valid",
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid with 'Issuer' issuerRef kind": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef: cmmeta.ObjectReference{
						Name: "valid",
						Kind: "Issuer",
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid with org set": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					Subject: &internalcmapi.X509Subject{
						Organizations: []string{"testorg"},
					},
					IssuerRef: validIssuerRef,
				},
			},
			a: someAdmissionRequest,
		},
		"invalid issuerRef kind": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef: cmmeta.ObjectReference{
						Name: "valid",
						Kind: "invalid",
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("issuerRef", "kind"), "invalid", "must be one of Issuer or ClusterIssuer"),
			},
		},
		"certificate missing secretName": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					IssuerRef:  validIssuerRef,
				},
			},
			errs: []*field.Error{
				field.Required(fldPath.Child("secretName"), "must be specified"),
			},
			a: someAdmissionRequest,
		},
		"certificate with no domains, URIs or common name": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath, "", "at least one of commonName, dnsNames, uris ipAddresses, or emailAddresses must be set"),
			},
		},
		"certificate with no issuerRef": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Required(fldPath.Child("issuerRef", "name"), "must be specified"),
			},
		},
		"valid certificate with only dnsNames": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					DNSNames:   []string{"validdnsname"},
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with rsa keyAlgorithm specified and no keySize": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.RSAKeyAlgorithm,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with rsa keyAlgorithm specified with keySize 2048": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.RSAKeyAlgorithm,
						Size:      2048,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with rsa keyAlgorithm specified with keySize 4096": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.RSAKeyAlgorithm,
						Size:      4096,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with rsa keyAlgorithm specified with keySize 8192": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.RSAKeyAlgorithm,
						Size:      8192,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with ecdsa keyAlgorithm specified and no keySize": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.ECDSAKeyAlgorithm,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with ecdsa keyAlgorithm specified with keySize 256": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Size:      256,
						Algorithm: internalcmapi.ECDSAKeyAlgorithm,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with ecdsa keyAlgorithm specified with keySize 384": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Size:      384,
						Algorithm: internalcmapi.ECDSAKeyAlgorithm,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with ecdsa keyAlgorithm specified with keySize 521": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Size:      521,
						Algorithm: internalcmapi.ECDSAKeyAlgorithm,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with keyAlgorithm not specified and keySize specified": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Size: 2048,
					},
				},
			},
			a: someAdmissionRequest,
		},
		"certificate with rsa keyAlgorithm specified and invalid keysize 1024": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.RSAKeyAlgorithm,
						Size:      1024,
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("privateKey", "size"), 1024, "must be between 2048 & 8192 for rsa keyAlgorithm"),
			},
		},
		"certificate with rsa keyAlgorithm specified and invalid keysize 8196": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.RSAKeyAlgorithm,
						Size:      8196,
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("privateKey", "size"), 8196, "must be between 2048 & 8192 for rsa keyAlgorithm"),
			},
		},
		"certificate with ecdsa keyAlgorithm specified and invalid keysize": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Size:      100,
						Algorithm: internalcmapi.ECDSAKeyAlgorithm,
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.NotSupported(fldPath.Child("privateKey", "size"), 100, []string{"256", "384", "521"}),
			},
		},
		"certificate with invalid keyAlgorithm": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					PrivateKey: &internalcmapi.CertificatePrivateKey{
						Algorithm: internalcmapi.PrivateKeyAlgorithm("blah"),
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("privateKey", "algorithm"), internalcmapi.PrivateKeyAlgorithm("blah"), "must be either empty or one of rsa or ecdsa"),
			},
		},
		"valid certificate with ipAddresses": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName:  "testcn",
					IPAddresses: []string{"127.0.0.1"},
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
			a: someAdmissionRequest,
		},
		"certificate with invalid ipAddresses": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName:  "testcn",
					IPAddresses: []string{"blah"},
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("ipAddresses").Index(0), "blah", "invalid IP address"),
			},
		},
		"valid certificate with commonName exactly 64 bytes": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "this-is-a-big-long-string-which-is-exactly-sixty-four-characters",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a:    someAdmissionRequest,
			errs: []*field.Error{},
		},
		"invalid certificate with commonName longer than 64 bytes": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "this-is-a-big-long-string-which-has-exactly-sixty-five-characters",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.TooLong(fldPath.Child("commonName"), "this-is-a-big-long-string-which-has-exactly-sixty-five-characters", 64),
			},
		},
		"valid certificate with no commonName and second dnsName longer than 64 bytes": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					DNSNames: []string{
						"dnsName",
						"this-is-a-big-long-string-which-has-exactly-sixty-five-characters",
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with commonName and first dnsName longer than 64 bytes": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					DNSNames: []string{
						"this-is-a-big-long-string-which-has-exactly-sixty-five-characters",
						"dnsName",
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with basic keyusage": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					Usages:     []internalcmapi.KeyUsage{"signing"},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with multiple keyusage": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					Usages:     []internalcmapi.KeyUsage{"signing", "s/mime"},
				},
			},
			a: someAdmissionRequest,
		},
		"invalid certificate with nonexistent keyusage": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					Usages:     []internalcmapi.KeyUsage{"nonexistent"},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("usages").Index(0), internalcmapi.KeyUsage("nonexistent"), "unknown keyusage"),
			},
		},
		"valid certificate with only URI SAN name": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
					URISANs: []string{
						"foo.bar",
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid certificate with only email SAN": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					EmailSANs:  []string{"alice@example.com"},
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
		},
		"invalid certificate with incorrect email": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					EmailSANs:  []string{"aliceexample.com"},
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("emailAddresses").Index(0), "aliceexample.com", "invalid email address: mail: missing '@' or angle-addr"),
			},
		},
		"invalid certificate with email formatted with name": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					EmailSANs:  []string{"Alice <alice@example.com>"},
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("emailAddresses").Index(0), "Alice <alice@example.com>", "invalid email address: make sure the supplied value only contains the email address itself"),
			},
		},
		"invalid certificate with email formatted with mailto": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					EmailSANs:  []string{"mailto:alice@example.com"},
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("emailAddresses").Index(0), "mailto:alice@example.com", "invalid email address: mail: expected comma"),
			},
		},
		"valid certificate with revision history limit == 1": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName:           "abc",
					SecretName:           "abc",
					IssuerRef:            validIssuerRef,
					RevisionHistoryLimit: int32Ptr(1),
				},
			},
			a: someAdmissionRequest,
		},
		"invalid certificate with revision history limit < 1": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName:           "abc",
					SecretName:           "abc",
					IssuerRef:            validIssuerRef,
					RevisionHistoryLimit: int32Ptr(0),
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("revisionHistoryLimit"), int32(0), "must not be less than 1"),
			},
		},
		"v1alpha2 certificate created": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "abc",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: &admissionv1.AdmissionRequest{
				RequestKind: &metav1.GroupVersionKind{Group: "cert-manager.io",
					Version: "v1alpha2",
					Kind:    "Certificate"},
			},
			warnings: validation.WarningList{
				fmt.Sprintf(deprecationMessageTemplate,
					cmapiv1alpha2.SchemeGroupVersion.String(),
					"Certificate",
					cmapi.SchemeGroupVersion.String(),
					"Certificate"),
			},
		},
		"v1alpha3 certificate created": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "abc",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: &admissionv1.AdmissionRequest{
				RequestKind: &metav1.GroupVersionKind{Group: "cert-manager.io",
					Version: "v1alpha3",
					Kind:    "Certificate"},
			},
			warnings: validation.WarningList{
				fmt.Sprintf(deprecationMessageTemplate,
					cmapiv1alpha3.SchemeGroupVersion.String(),
					"Certificate",
					cmapi.SchemeGroupVersion.String(),
					"Certificate"),
			},
		},
		"v1beta1 certificate created": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "abc",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
			a: &admissionv1.AdmissionRequest{
				RequestKind: &metav1.GroupVersionKind{Group: "cert-manager.io",
					Version: "v1beta1",
					Kind:    "Certificate"},
			},
			warnings: validation.WarningList{
				fmt.Sprintf(deprecationMessageTemplate,
					cmapiv1beta1.SchemeGroupVersion.String(),
					"Certificate",
					cmapi.SchemeGroupVersion.String(),
					"Certificate"),
			},
		},
		"valid with empty secretTemplate": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					SecretTemplate: &internalcmapi.CertificateSecretTemplate{
						Annotations: map[string]string{},
						Labels:      map[string]string{},
					},
					IssuerRef: cmmeta.ObjectReference{
						Name: "valid",
					},
				},
			},
			a: someAdmissionRequest,
		},
		"valid with 'CertificateSecretTemplate' labels and annotations": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					SecretTemplate: &internalcmapi.CertificateSecretTemplate{
						Annotations: map[string]string{
							"my-annotation.com/foo": "app=bar",
						},
						Labels: map[string]string{
							"my-label.com/foo": "evn-production",
						},
					},
					IssuerRef: cmmeta.ObjectReference{
						Name: "valid",
					},
				},
			},
			a: someAdmissionRequest,
		},
		"invalid with disallowed 'CertificateSecretTemplate' annotations": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					SecretTemplate: &internalcmapi.CertificateSecretTemplate{
						Annotations: map[string]string{
							"app.com/valid":                    "valid",
							"cert-manager.io/alt-names":        "example.com",
							"cert-manager.io/certificate-name": "selfsigned-cert",
						},
					},
					IssuerRef: cmmeta.ObjectReference{
						Name: "invalid",
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(fldPath.Child("secretTemplate", "annotations"), "cert-manager.io/alt-names", "cert-manager.io/* annotations are not allowed"),
				field.Invalid(fldPath.Child("secretTemplate", "annotations"), "cert-manager.io/certificate-name", "cert-manager.io/* annotations are not allowed"),
			},
		},
		"invalid due to too long 'CertificateSecretTemplate' annotations": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					SecretTemplate: &internalcmapi.CertificateSecretTemplate{
						Annotations: map[string]string{
							"app.com/invalid": strings.Repeat("0", maxSecretTemplateAnnotationsBytesLimit),
						},
					},
					IssuerRef: cmmeta.ObjectReference{
						Name: "invalid",
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.TooLong(fldPath.Child("secretTemplate", "annotations"), "", maxSecretTemplateAnnotationsBytesLimit),
			},
		},
		"invalid due to not allowed 'CertificateSecretTemplate' labels": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					SecretTemplate: &internalcmapi.CertificateSecretTemplate{
						Labels: map[string]string{
							"app.com/invalid-chars": "invalid=chars",
						},
					},
					IssuerRef: cmmeta.ObjectReference{
						Name: "invalid",
					},
				},
			},
			a: someAdmissionRequest,
			errs: []*field.Error{
				field.Invalid(
					fldPath.Child("secretTemplate", "labels"),
					"invalid=chars", "a valid label must be an empty string or consist of alphanumeric characters, '-', '_' or '.', and must start and end with an "+
						"alphanumeric character (e.g. 'MyValue',  or 'my_value',  or '12345', regex used for validation is '(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?')"),
			},
		},
	}
	for n, s := range scenarios {
		t.Run(n, func(t *testing.T) {
			errs, warnings := ValidateCertificate(s.a, s.cfg)
			assert.ElementsMatch(t, errs, s.errs)
			assert.ElementsMatch(t, warnings, s.warnings)
		})
	}
}

func TestValidateDuration(t *testing.T) {
	usefulDurations := map[string]*metav1.Duration{
		"one second":  {Duration: time.Second},
		"ten minutes": {Duration: time.Minute * 10},
		"half hour":   {Duration: time.Minute * 30},
		"one hour":    {Duration: time.Hour},
		"one month":   {Duration: time.Hour * 24 * 30},
		"half year":   {Duration: time.Hour * 24 * 180},
		"one year":    {Duration: time.Hour * 24 * 365},
		"ten years":   {Duration: time.Hour * 24 * 365 * 10},
	}

	fldPath := field.NewPath("spec")
	scenarios := map[string]struct {
		cfg  *internalcmapi.Certificate
		errs []*field.Error
	}{
		"default duration and renewBefore": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
		},
		"valid duration and renewBefore": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					Duration:    usefulDurations["one year"],
					RenewBefore: usefulDurations["half year"],
					CommonName:  "testcn",
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
		},
		"unset duration, valid renewBefore for default": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					RenewBefore: usefulDurations["one month"],
					CommonName:  "testcn",
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
		},
		"unset renewBefore, valid duration for default": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					Duration:   usefulDurations["one year"],
					CommonName: "testcn",
					SecretName: "abc",
					IssuerRef:  validIssuerRef,
				},
			},
		},
		"renewBefore is bigger than the default duration": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					RenewBefore: usefulDurations["ten years"],
					CommonName:  "testcn",
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
			errs: []*field.Error{field.Invalid(fldPath.Child("renewBefore"), usefulDurations["ten years"].Duration, fmt.Sprintf("certificate duration %s must be greater than renewBefore %s", cmapi.DefaultCertificateDuration, usefulDurations["ten years"].Duration))},
		},
		"renewBefore is bigger than the duration": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					Duration:    usefulDurations["one month"],
					RenewBefore: usefulDurations["one year"],
					CommonName:  "testcn",
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
			errs: []*field.Error{field.Invalid(fldPath.Child("renewBefore"), usefulDurations["one year"].Duration, fmt.Sprintf("certificate duration %s must be greater than renewBefore %s", usefulDurations["one month"].Duration, usefulDurations["one year"].Duration))},
		},
		"renewBefore is less than the minimum permitted value": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					RenewBefore: usefulDurations["one second"],
					CommonName:  "testcn",
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
			errs: []*field.Error{field.Invalid(fldPath.Child("renewBefore"), usefulDurations["one second"].Duration, fmt.Sprintf("certificate renewBefore must be greater than %s", cmapi.MinimumRenewBefore))},
		},
		"duration is less than the minimum permitted value": {
			cfg: &internalcmapi.Certificate{
				Spec: internalcmapi.CertificateSpec{
					Duration:    usefulDurations["half hour"],
					RenewBefore: usefulDurations["ten minutes"],
					CommonName:  "testcn",
					SecretName:  "abc",
					IssuerRef:   validIssuerRef,
				},
			},
			errs: []*field.Error{field.Invalid(fldPath.Child("duration"), usefulDurations["half hour"].Duration, fmt.Sprintf("certificate duration must be greater than %s", cmapi.MinimumCertificateDuration))},
		},
	}
	for n, s := range scenarios {
		t.Run(n, func(t *testing.T) {
			errs := ValidateDuration(&s.cfg.Spec, fldPath)
			assert.ElementsMatch(t, errs, s.errs)
		})
	}
}
