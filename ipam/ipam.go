package ipam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	etcd "go.etcd.io/etcd/client/v3"
	"log"
	"net"
)

/*
! ip分配器: 用etcd保存.
1. 快速分配节点未使用的IP.
2. 分配节点的输入信息不管是上层还是本地配置, 我们用{网段}作为基本输入.

假设需要分配IP -> 需要知道哪些IP可用 -> 哪些IP已经使用. 设计如下:

key: /zhongcni/ipam/{网段}/
value: ip1; ip2
含义: 该网段已经使用的ip.

key: /zhongcni/ipam/{实例名}
value: {网段, ip}
含义: 该节点使用的网段和ip.
*/

/*
! golang中的面向对象:
面向对象的三大基本特性：
封装: struct
继承: 在语言设计上采取的是组合的方式, 声明为成员
多态: interface
*/

const (
	ipam    = "ipam"
	cniName = "zhongcni"
)

type NetInfo struct {
	NetSegment  string   `json:"netSegment,omitempty"` // alt+ enter生成
	AddressUsed []string `json:"addressUsed,omitempty"`
}

type InstanceNetInfo struct {
	Instance   string   `json:"instance,omitempty"`
	NetSegment string   `json:"netSegment,omitempty"`
	Addresses  []string `json:"addresses,omitempty"`
}

/*
! 命名规范
变量名/函数名/类型名: 驼峰
私有: 小写开头
包名: 下划线
常量: 看作变量, 包私有, 小写开头
*/

type IPAMService struct {
	etcdClient *etcd.Client
}

func NewIPAMService(etcdClient *etcd.Client) *IPAMService {
	return &IPAMService{etcdClient: etcdClient}
}

func (s IPAMService) getNetInfoKey(netSegment string) string {
	return fmt.Sprintf("/%v/%v/%v", cniName, ipam, netSegment)
}

func (s IPAMService) getInstanceNetInfoKey(instance string) string {
	return fmt.Sprintf("/%v/%v/%v", cniName, ipam, instance)
}

/*

! etcd:
revision
	全局单调递增的数字,任何key的增删改都会触发增加
	存在于resp的header中

createrevision
	创建该key时候的revision,在删除前不会改变
	存在于resp的Kvs中

modrevision
	修改该key时候的revision,每次更新都会触发增加
	存在于resp的Kvs中

version
	此key截至目前的修改次数
	存在于resp的Kvs中
*/

/*
type CNI interface {
	Setup(ctx context.Context, id string, path string, opts ...NamespaceOpts) (*Result, error)
	SetupSerially(ctx context.Context, id string, path string, opts ...NamespaceOpts) (*Result, error)
	Remove(ctx context.Context, id string, path string, opts ...NamespaceOpts) error
	Check(ctx context.Context, id string, path string, opts ...NamespaceOpts) error
	Load(opts ...Opt) error
	Status() error
	GetConfig() *ConfigResult
}

在containerd中, cni调用分配Setup后, defer了失败则Remove. 尽管如此, 却依然存在失败的情况. 比如进程被kill.

! 对于自带ipam的开源网络框架,会周期性扫描泄露的ip:

func (c *ipamController) checkAllocations() ([]string, error)

这段代码是针对 Calico IPAM 的自动垃圾回收机制的实现。
首先，该函数会扫描所有的 IP 分配情况，并检查每个 IP 分配是否有泄漏的情况。检查泄漏的情况分为两种情况：
1. 对应的 Pod 在 Kubernetes API 中不存在
2. 对应的 Pod 存在，但是其 IP 与分配的 IP 不匹配
*/

/*
! k8s自带的ipam: https://www.cni.dev/plugins/current/ipam/
plugins/ipam/dhcp
plugins/ipam/host-local: 容器的IPv4和IPv6地址将从指定的地址范围中分配。这个地址范围可以在CNI配置文件中定义.
plugins/ipam/static: 自己写死固定在配置, 详细见链接
*/

func (s IPAMService) AllocateAddress(netSegment string) (string, error) {
	for {
		resp, err := s.etcdClient.Get(context.Background(), s.getNetInfoKey(netSegment))
		if err != nil {
			return "", err
		}

		netInfo, ipAllocate, err := s.genNetInfo(netSegment, resp)
		if err != nil {
			return "", err
		}

		netInfoJson, err := json.Marshal(netInfo)
		if err != nil {
			return "", err
		}

		// 使用cas进行分配
		key := s.getNetInfoKey(netSegment)
		txnResp, err := s.etcdClient.KV.Txn(context.Background()).If(
			etcd.Compare(etcd.ModRevision(key), "=", resp.Kvs[0].ModRevision),
		).Then(
			etcd.OpPut(key, string(netInfoJson)),
		).Else(
			etcd.OpGet(key),
		).Commit()

		if err != nil {
			log.Println("Txn failed for %s", key)
			return "", err
		}

		if !txnResp.Succeeded {
			log.Printf("deletion of %s failed because of a conflict, going to retry\n", key)
			continue
		}
		log.Printf("success to AllocateAddress, key: %s, ip: %s\n", key, ipAllocate)
		return ipAllocate, nil
	}
}

func (s IPAMService) genNetInfo(netSegment string, resp *etcd.GetResponse) (NetInfo, string, error) {
	var netInfo NetInfo
	var ipAllocate string
	if len(resp.Kvs) == 0 {
		ip, err := getAvailableIPBySeg(netSegment, []string{})
		if err != nil {
			return NetInfo{}, "", err
		}
		ipAllocate = ip
		netInfo.NetSegment = netSegment
		netInfo.AddressUsed = []string{ip}
	} else {
		if err := json.Unmarshal(resp.Kvs[0].Value, &netInfo); err != nil {
			return NetInfo{}, "", err
		}
		ip, err := getAvailableIPBySeg(netSegment, netInfo.AddressUsed)
		if err != nil {
			return NetInfo{}, "", err
		}
		ipAllocate = ip
		netInfo.AddressUsed = append(netInfo.AddressUsed, ip)
	}
	return netInfo, ipAllocate, nil
}

func getAvailableIPBySeg(netSegment string, usedIps []string) (string, error) {
	_, ipNet, err := net.ParseCIDR(netSegment)
	if err != nil {
		return "", err
	}
	// 排除网段自己
	usedIps = append(usedIps, ipNet.IP.String())
	used := make([]net.IP, 0)
	for _, ipStr := range usedIps {
		used = append(used, net.ParseIP(ipStr))
	}
	address, err := getAvailableIP(ipNet, used)
	if err != nil {
		return "", err
	}
	return address.String(), err
}

func getAvailableIP(ipNet *net.IPNet, used []net.IP) (net.IP, error) {
	for ip := ipNet.IP.Mask(ipNet.Mask); ipNet.Contains(ip); {
		if !contains(used, ip) {
			return ip, nil
		}
		incrementIP(ip)
	}
	return nil, errors.New("no available IP address in the given range")
}

func contains(ips []net.IP, target net.IP) bool {
	for _, ip := range ips {
		if ip.Equal(target) {
			return true
		}
	}
	return false
}

func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] > 0 {
			break
		}
	}
}
