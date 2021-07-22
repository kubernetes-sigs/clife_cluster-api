/*
Copyright 2021 The Kubernetes Authors.

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

package kubemark

import (
	"context"
	"crypto"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	"sigs.k8s.io/cluster-api/controllers/remote"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (k *kubemark) createSharedSecret(ctx context.Context) error {
	key := sharedSecretKey(k.hollowMachine)
	sharedSecret := &corev1.Secret{}
	if err := k.externalClusterClient.Get(ctx, key, sharedSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get hollow machine kubeconfig Secret")
		}
	}
	if sharedSecret.Name != "" {
		return nil
	}

	caCert, caKey, err := k.getClusterCA(ctx)
	if err != nil {
		return err
	}

	kubeProxyCert, kubeProxyKey, err := k.generateClientCertificate(caCert, caKey, x509.Certificate{
		Subject: pkix.Name{
			CommonName: "system:kube-proxy",
		},
	})
	if err != nil {
		return err
	}
	kubeProxyKubeConfig, err := k.generateKubeconfig(ctx, caCert, kubeProxyCert, kubeProxyKey)
	if err != nil {
		return err
	}

	sharedSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace,
			Name:      key.Name,
		},
		Data: map[string][]byte{
			"kubeproxy.kubeconfig": kubeProxyKubeConfig,
		},
	}
	if err := k.externalClusterClient.Create(ctx, sharedSecret); err != nil {
		return errors.Wrap(err, "failed to create kubemark Secret")
	}
	return nil
}

func (k *kubemark) createPodSecret(ctx context.Context) error {
	key := podSecretKey(k.hollowMachine)
	podSecret := &corev1.Secret{}
	if err := k.externalClusterClient.Get(ctx, key, podSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get hollow machine kubeconfig Secret")
		}
	}
	if podSecret.Name != "" {
		return nil
	}

	caCert, caKey, err := k.getClusterCA(ctx)
	if err != nil {
		return err
	}

	kubeletCert, kubeletKey, err := k.generateClientCertificate(caCert, caKey, x509.Certificate{
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("system:node:%s", HollowNodeName(k.hollowMachine)),
			Organization: []string{"system:nodes"},
		},
	})
	if err != nil {
		return err
	}
	kubeletKubeConfig, err := k.generateKubeconfig(ctx, caCert, kubeletCert, kubeletKey)
	if err != nil {
		return err
	}

	podSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace,
			Name:      key.Name,
		},
		Data: map[string][]byte{
			"kubelet.kubeconfig": kubeletKubeConfig,
		},
	}
	if err := k.externalClusterClient.Create(ctx, podSecret); err != nil {
		return errors.Wrap(err, "failed to create kubemark Secret")
	}
	return nil
}

func (k *kubemark) generateClientCertificate(caCert *x509.Certificate, caKey crypto.Signer, certSpec x509.Certificate) (*x509.Certificate, crypto.Signer, error) {
	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create kubemark certificate key")
	}

	serial, err := cryptorand.Int(cryptorand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to compute serial for kubemark certificate")
	}

	certSpec.SerialNumber = serial
	certSpec.NotBefore = caCert.NotBefore
	certSpec.NotAfter = time.Now().Add(time.Hour * 24 * 365).UTC()
	certSpec.KeyUsage = x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature
	certSpec.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}

	certBytes, err := x509.CreateCertificate(cryptorand.Reader, &certSpec, caCert, key.Public(), caKey)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create kubemark certificate")
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to parse generated kubemark certificate")
	}
	return cert, key, nil
}

func (k *kubemark) generateKubeconfig(ctx context.Context, caCert *x509.Certificate, cert *x509.Certificate, key crypto.Signer) ([]byte, error) {
	restConfig, err := remote.RESTConfig(ctx, "", k.managementClusterClient, client.ObjectKeyFromObject(k.cluster))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get cluster RESTConfig")
	}

	// Build resulting kubeconfig.
	kubeConfig := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"cluster": {
			Server:                   restConfig.Host,
			CertificateAuthorityData: encodeCertPEM(caCert),
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"user": {
			ClientCertificateData: encodeCertPEM(cert),
			ClientKeyData:         encodeKeyPEM(key),
		}},
		Contexts: map[string]*clientcmdapi.Context{"default": {
			Cluster:  "cluster",
			AuthInfo: "user",
		}},
		CurrentContext: "default",
	}

	kubeConfigBytes, err := runtime.Encode(clientcmdlatest.Codec, kubeConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode kubemark KubeConfig")
	}
	return kubeConfigBytes, nil
}

func (k *kubemark) getClusterCA(ctx context.Context) (*x509.Certificate, crypto.Signer, error) {
	clusterCertificates := secret.Certificates{
		&secret.Certificate{
			Purpose: secret.ClusterCA,
		},
	}

	if err := clusterCertificates.Lookup(ctx, k.managementClusterClient, client.ObjectKeyFromObject(k.cluster)); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to get cluster CA")
	}

	if err := clusterCertificates.EnsureAllExist(); err != nil {
		return nil, nil, errors.Wrapf(err, "cluster CA does not exists")
	}

	clusterCA := clusterCertificates.GetByPurpose(secret.ClusterCA)

	caCert, err := certs.DecodeCertPEM(clusterCA.KeyPair.Cert)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to decode cluster CA certificate")
	}

	caKey, err := certs.DecodePrivateKeyPEM(clusterCA.KeyPair.Key)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to decode cluster CA key")
	}
	return caCert, caKey, nil
}

func (k *kubemark) deletePodSecret(ctx context.Context) error {
	key := podSecretKey(k.hollowMachine)
	podSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace,
			Name:      key.Name,
		},
	}
	if err := k.externalClusterClient.Delete(ctx, podSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to delete kubemark Secret")
		}
	}
	return nil
}

func sharedSecretKey(hollowMachine *infrav1.HollowMachine) client.ObjectKey {
	return client.ObjectKey{
		Namespace: hollowMachine.Namespace,
		Name:      "hollow-machines",
	}
}

func podSecretKey(hollowMachine *infrav1.HollowMachine) client.ObjectKey {
	return client.ObjectKey{
		Namespace: hollowMachine.Namespace,
		Name:      hollowMachine.Name,
	}
}

func encodeCertPEM(cert *x509.Certificate) []byte {
	block := pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(&block)
}

func encodeKeyPEM(privateKey crypto.Signer) []byte {
	switch t := privateKey.(type) {
	case *rsa.PrivateKey:
		block := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(t),
		}
		return pem.EncodeToMemory(block)
	default:
		return nil
	}
}
