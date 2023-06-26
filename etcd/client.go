package etcd

import (
	"go.etcd.io/etcd/client/pkg/v3/transport"
	"go.etcd.io/etcd/client/v3"
	"time"
)

func GetEtcdClient() (*clientv3.Client, error) {
	tlsInfo := transport.TLSInfo{
		CertFile:      "C:\\all\\tmp_download\\kubernetes.pem",
		KeyFile:       "C:\\all\\tmp_download\\kubernetes-key.pem",
		TrustedCAFile: "C:\\all\\tmp_download\\ca.pem",
	}
	clientTimeout := 30 * time.Second
	tlsConfig, err := tlsInfo.ClientConfig()
	if err != nil {
		return nil, err
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"192.168.5.11:2379"},
		TLS:         tlsConfig,
		DialTimeout: clientTimeout,
	})
	return client, nil
}
