package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	httplogger "github.com/jesseokeya/go-httplogger"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type PodItem struct {
	Name    string      `json:"name"`
	Created string      `json:"created"`
	Status  v1.PodPhase `json:"status"`
}

// App is an app
type App struct {
	clientset  *kubernetes.Clientset
	kubeconfig string
	namespace  string
}

func (a *App) buildClientSet(context string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: a.kubeconfig},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	return clientset, err
}

func parseRequired(request *http.Request) (string, string) {
	params := request.URL.Query()
	context := params.Get("context")
	namespace := params.Get("namespace")
	return context, namespace
}

func (a *App) listContext() []string {
	data, err := clientcmd.LoadFromFile(a.kubeconfig)
	if err != nil {
		panic(err)
	}
	result := []string{}
	for key := range data.Contexts {
		result = append(result, key)
	}
	return result
}

func (a *App) startClient(kubeconfig string) {
	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	a.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
}

func (a *App) getAllPods(contains string, request *http.Request) []byte {
	context, namespace := parseRequired(request)
	clientset, err := a.buildClientSet(context)
	if err != nil {
		panic(err)
	}
	podItems := []PodItem{}
	pods, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	podsList := pods.Items
	for _, pod := range podsList {
		podItem := PodItem{
			Name:    pod.GetName(),
			Created: pod.GetCreationTimestamp().String(),
			Status:  pod.Status.Phase,
		}
		podItems = append(podItems, podItem)
	}
	podsListJSON, err := json.MarshalIndent(podItems, "", "    ")
	if err != nil {
		panic(err)
	}
	return podsListJSON
}

func (a *App) getPodLogs(podName string, request *http.Request) []byte {
	context, namespace := parseRequired(request)
	clientset, err := a.buildClientSet(context)
	if err != nil {
		panic(err)
	}
	podLogOpts := v1.PodLogOptions{}
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &podLogOpts)
	podLogs, err := req.Stream()
	if err != nil {
		return []byte("error in opening stream")
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return []byte("error in copy information from podLogs to buf")
	}
	result := buf.Bytes()

	return result
}

func (a *App) getLatestPod(request *http.Request) []byte {
	context, namespace := parseRequired(request)
	clientset, err := a.buildClientSet(context)
	if err != nil {
		panic(err)
	}
	timezone, err := time.LoadLocation("CET")
	if err != nil {
		panic(err)
	}
	initialDate := time.Date(int(1980), time.Month(1), int(12), int(0), int(0), int(0), int(0), timezone).Unix()
	var currentPod v1.Pod
	pods, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, pod := range pods.Items {
		if !strings.Contains(pod.GetName(), "batch") {
			continue
		}
		if pod.GetCreationTimestamp().Unix() < initialDate {
			continue
		}

		initialDate = pod.GetCreationTimestamp().Unix()
		currentPod = pod
	}
	responseItem := PodItem{
		Name:    currentPod.GetName(),
		Created: currentPod.GetCreationTimestamp().String(),
		Status:  currentPod.Status.Phase,
	}
	podJSON, err := json.MarshalIndent(
		[]PodItem{responseItem},
		"",
		"    ",
	)
	return podJSON
}

func (a *App) listNamespace(request *http.Request) []string {
	context, _ := parseRequired(request)
	clientset, err := a.buildClientSet(context)
	if err != nil {
		panic(err)
	}
	results := []string{}
	namespaces, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{})
	for _, namespace := range namespaces.Items {
		results = append(results, namespace.GetName())
	}
	return results
}

func (a *App) handleGetPods(response http.ResponseWriter, request *http.Request) {
	params := request.URL.Query()
	filter := params.Get("filter")
	podList := a.getAllPods(filter, request)
	response.WriteHeader(200)
	response.Write(podList)
}

func (a *App) handleGetLogs(response http.ResponseWriter, request *http.Request) {
	params := request.URL.Query()
	podName := params.Get("name")
	podLogs := a.getPodLogs(podName, request)
	response.WriteHeader(200)
	response.Write(podLogs)
}

func (a *App) handleLatestPod(response http.ResponseWriter, request *http.Request) {
	pod := a.getLatestPod(request)
	response.WriteHeader(200)
	response.Write(pod)
}

func (a *App) handleListContext(response http.ResponseWriter, request *http.Request) {
	contexts := a.listContext()
	contextJSON, err := json.MarshalIndent(
		contexts,
		"",
		"    ",
	)
	if err != nil {
		panic(err)
	}
	response.WriteHeader(200)
	response.Write(contextJSON)
}

func (a *App) handleListNamespaces(response http.ResponseWriter, request *http.Request) {
	namespaces := a.listNamespace(request)
	namespacesJSON, err := json.MarshalIndent(
		namespaces,
		"",
		"    ",
	)
	if err != nil {
		panic(err)
	}
	response.WriteHeader(200)
	response.Write(namespacesJSON)
}

type appError struct {
	Error   error
	Message string
	Code    int
}

type Handler func(http.ResponseWriter, *http.Request)

func (fn Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	fn(w, r)
}

func main() {
	var kubeconfigFlag *string
	if home := homeDir(); home != "" {
		kubeconfigFlag = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfigFlag = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	kubeconfig := *kubeconfigFlag

	port := 4500
	app := App{
		namespace:  "default",
		kubeconfig: kubeconfig,
	}
	app.startClient(kubeconfig)
	mux := http.NewServeMux()
	mux.Handle("/pods", Handler(app.handleGetPods))
	mux.Handle("/logs", Handler(app.handleGetLogs))
	mux.Handle("/latest", Handler(app.handleLatestPod))
	mux.Handle("/context", Handler(app.handleListContext))
	mux.Handle("/namespaces", Handler(app.handleListNamespaces))
	fmt.Printf("Starting Server on localhost:%d\n", port)
	http.ListenAndServe(fmt.Sprintf(":%d", port), httplogger.Golog(mux))
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
