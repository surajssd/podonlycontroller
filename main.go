package main

import (
	"fmt"
	"log"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"

	"github.com/surajssd/podonly/utility"
)

func main() {

	stopCh := utility.SetupSignalHandler()

	kubeconfig := os.ExpandEnv("$HOME/.kube/config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("[!] could not build client: %v", err)
	}
	log.Println("[*] found k8s configs")

	podClient := kubernetes.NewForConfigOrDie(config)
	podInformerFactory := kubeinformers.NewSharedInformerFactory(podClient, time.Second*30)
	podInformer := podInformerFactory.Core().V1().Pods()

	pc := NewPodController(podInformer)

	go podInformerFactory.Start(stopCh)

	if err = pc.Run(stopCh); err != nil {
		log.Fatalf("[!] error running controller: %v", err)
	}

	log.Println("[*] starting the worker")

}

type PodController struct {
	queue  workqueue.RateLimitingInterface
	lister corelister.PodLister
	synced cache.InformerSynced
}

func (c *PodController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log.Println("[*] starting the pod controller")

	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("[!] failed to wait for caches to sync")
	}
	go wait.Until(c.worker, time.Second, stopCh)

	log.Println("[*] workers started")
	<-stopCh
	log.Println("[*] shutting down workers")

	return nil
}

////////////////////////////////////
// Everything below this is done ///
////////////////////////////////////

// worker will keep processing the items from the queue
func (c *PodController) worker() {
	for c.processNextItem() {
		log.Println("[*] Processed one item in the queue")
	}
}

// processNextItem will process the items in the queue
func (c *PodController) processNextItem() bool {
	item, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	key, ok := item.(string)
	if !ok {
		c.handleError(fmt.Errorf("[!] expected string in workqueue but got %#v", item), item)
		return true
	}

	err := c.syncHandler(key)
	c.handleError(err, item)

	c.queue.Done(item)
	return true
}

func (c *PodController) handleError(err error, item interface{}) {
	log.Fatalf("[!] got error: %q on object: %#v", err, item)
}

func (c *PodController) syncHandler(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	podTmp, err := c.lister.Pods(ns).Get(name)
	if err != nil {
		return err
	}
	log.Printf("[*] Here is the pod: %#v", podTmp)
	return nil
}

func NewPodController(podInformer coreinformers.PodInformer) *PodController {

	pc := &PodController{
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(), "pods",
		),
		lister: podInformer.Lister(),
		synced: podInformer.Informer().HasSynced,
	}

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    pc.addFunc,
		DeleteFunc: pc.delFunc,
	})

	return pc
}

func (c *PodController) addFunc(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		log.Println("[!] could not convert to pod object")
		return
	}
	log.Println("[*] started new pod:", pod.Name)
}

func (c *PodController) delFunc(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		log.Println("[!] could not convert to pod object")
		return
	}
	log.Println("[*] deleting the pod:", pod.Name)
}
