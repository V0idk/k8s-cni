package ipam

import (
	etcd "k8s-cni/etcd"
	"testing"
)

func TestIpam(t *testing.T) {
	client, _ := etcd.GetEtcdClient()
	s := NewIPAMService(client)
	s.AllocateAddress("10.240.0.0/24")
}
