package main

// https://azure.github.io/azure-workload-identity/docs/topics/service-account-labels-and-annotations.html

// https://kubernetes.io/docs/reference/kubectl/generated/kubectl_create/kubectl_create_token/
// kubectl create token service-account-aks-workload -n argocd --audience "api://AzureADTokenExchange"

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

const AzureDevOpsResourceId = "499b84ac-1321-427f-aa17-267ca6975798"
const AzureEntraIDEndpoint = "https://login.microsoftonline.com/"

func main() {

	argocdNamespaceName := os.Getenv("ARGOCD_NAMESPACE")
	if argocdNamespaceName == "" {
		panic("ARGOCD_NAMESPACE is not set")
	}

	secretName := os.Getenv("ARGOCD_SECRET")
	if secretName == "" {
		panic("ARGOCD_SECRET is not set")
	}

	serviceAccountName := os.Getenv("ARGOCD_SA")
	if serviceAccountName == "" {
		panic("ARGOCD_SA is not set")
	}

	_, err := UpdateAccessToken(argocdNamespaceName, serviceAccountName, secretName)
	if err != nil {
		panic(err.Error())
	}
	fmt.Println("Entra ID Token generation completed")
}

func UpdateAccessToken(argocdNamespaceName string, serviceAccountName string, secretName string) (*corev1.Secret, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	client := clientset.CoreV1()

	tenantID, clientID, err := FetchWorkloadIdentityInfo(argocdNamespaceName, serviceAccountName, client)
	if err != nil {
		return nil, err
	}

	saToken, err := FetchSAToken(context.TODO(), argocdNamespaceName, serviceAccountName, []string{"api://AzureADTokenExchange"}, client)
	if err != nil {
		return nil, err
	}

	token, err := FetchEntraIdAccessToken(tenantID, clientID, saToken)
	if err != nil {
		return nil, err
	}

	secret, err := PatchSecret(argocdNamespaceName, secretName, token, client)
	if err != nil {
		return nil, err
	}

	return secret, nil
}

func FetchSAToken(ctx context.Context, ns, name string, audiences []string, kubeClient kcorev1.CoreV1Interface) (string, error) {
	token, err := kubeClient.ServiceAccounts(ns).CreateToken(ctx, name, &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences: audiences,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return token.Status.Token, nil
}

func FetchWorkloadIdentityInfo(argocdNamespaceName string, serviceAccountName string, kubeClient kcorev1.CoreV1Interface) (string, string, error) {
	sa, err := kubeClient.ServiceAccounts(argocdNamespaceName).Get(context.TODO(), serviceAccountName, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	tenantID, ok := sa.Annotations["azure.workload.identity/tenant-id"]
	if !ok {
		return "", "", fmt.Errorf("missing annotation 'azure.workload.identity/tenant-id' in service account '%s'", serviceAccountName)
	}
	clientID, ok := sa.Annotations["azure.workload.identity/client-id"]
	if !ok {
		return "", "", fmt.Errorf("missing annotation 'azure.workload.identity/client-id' in service account '%s'", serviceAccountName)
	}
	return tenantID, clientID, nil
}

func FetchEntraIdAccessToken(tenantID string, clientID string, saToken string) (string, error) {
	cred := confidential.NewCredFromAssertionCallback(func(ctx context.Context, aro confidential.AssertionRequestOptions) (string, error) {
		return saToken, nil
	})
	cClient, err := confidential.New(fmt.Sprintf("%s%s/oauth2/token", AzureEntraIDEndpoint, tenantID), clientID, cred)
	if err != nil {
		return "", err
	}
	scope := AzureDevOpsResourceId
	// .default needs to be added to the scope
	if !strings.Contains(AzureDevOpsResourceId, ".default") {
		scope = fmt.Sprintf("%s/.default", AzureDevOpsResourceId)
	}
	authRes, err := cClient.AcquireTokenByCredential(context.TODO(), []string{
		scope,
	})
	if err != nil {
		return "", err
	}
	return authRes.AccessToken, nil
}

func PatchSecret(argocdNamespaceName string, secretName string, token string, client kcorev1.CoreV1Interface) (*corev1.Secret, error) {
	patchData := map[string]interface{}{
		"data": map[string]string{
			"password": base64.StdEncoding.EncodeToString([]byte(token)),
		},
	}

	// Convert patch data to JSON
	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return nil, err
	}

	return client.Secrets(argocdNamespaceName).Patch(
		context.TODO(),
		secretName,
		types.StrategicMergePatchType,
		patchBytes,
		metav1.PatchOptions{})
}
