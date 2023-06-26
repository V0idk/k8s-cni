package main

import (
	"context"
	"fmt"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
	etcd "go.etcd.io/etcd/client/v3"
	cnietcd "k8s-cni/etcd"
	"log"
	"os"
	"time"
)

func cmdAdd(args *skel.CmdArgs) error {
	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func cmdDel(args *skel.CmdArgs) error {
	return nil
}

func waitDebugAttach() {
	var actuallyRun = false
	var startTime = time.Now()
	var duration = 5 * time.Minute
	for time.Since(startTime) < duration {
		// change this in debug
		if actuallyRun {
			break
		}
		// 防止golang优化
		if time.Since(startTime) > 6*time.Minute {
			actuallyRun = true
		}
		fmt.Println("waiting connect: ", os.Getpid())
		time.Sleep(1 * time.Second)
	}
	fmt.Println("connect success")
}

/*
! 特定pod运行
kubectl run busybox1 --overrides='{"spec": { "nodeSelector": {"kubernetes.io/hostname": "vm1"}}}' --image=busybox:1.28 --command -- sleep 3600
dlv attach 68506 --listen=:8080 --headless=true --api-version=2

! cni文件配置
root@vm1:/etc/cni/net.d# cat 9-testcni.conf
{
        "cniVersion": "0.3.0",
        "name": "testcni",
        "type": "testcni",
        "bridge": "testcni0",
        "subnet": "10.244.0.0/16"
}

! 编译方法
SET GOOS=linux
SET GOARCH=amd64
go build -o main main.go
*/

func printAllKey(client *etcd.Client) {
	resp, err := client.Get(context.Background(), "/", etcd.WithPrefix())
	if err != nil {
		fmt.Println("Get error:", err)
		return
	}

	for _, kv := range resp.Kvs {
		fmt.Printf("=================================================\n")
		fmt.Printf("Key: %s\n", kv.Key)
		fmt.Printf("Value: %s\n", kv.Value)
	}
}

func main() {
	file, err := os.OpenFile("testcni.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(file)

	client, _ := cnietcd.GetEtcdClient()
	printAllKey(client)

	waitDebugAttach()
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, "testcni")
}
