package ipip

import (
	"crypto/rand"
	"fmt"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"net"
	"os"
)

/*

同节点通信

*/

type IpipCNI struct{}

func getRandomStr(length int) (string, error) {
	str := make([]byte, length)
	_, err := rand.Read(str)
	if err != nil {
		return "", fmt.Errorf("failed to generate random str: %v", err)
	}
	return fmt.Sprintf("%x", str), nil

}

// 获取跟本地节点不冲突的veth名
func getVethNameOnHost() (string, error) {
	for {
		str, err := getRandomStr(4)
		if err != nil {
			return "", err
		}
		veth := fmt.Sprintf("veth%v", str)
		_, err = netlink.LinkByName(veth)
		if err != nil {
			return veth, nil
		}
	}
}

func getVethByName(name string) (*netlink.Veth, error) {
	veth, err := netlink.LinkByName(name)
	if err != nil {
		return nil, err
	}
	return veth.(*netlink.Veth), nil
}

func (ipip *IpipCNI) Add(args *skel.CmdArgs) error {
	args = &skel.CmdArgs{
		ContainerID: "308102901b7fe9538fcfc71669d505bc09f9def5eb05adeddb73a948bb4b2c8b",
		Netns:       "/var/run/netns/ns3",
		IfName:      "eth0",
		Args:        "K8S_POD_INFRA_CONTAINER_ID=308102901b7fe9538fcfc71669d505bc09f9def5eb05adeddb73a948bb4b2c8b;K8S_POD_UID=d392609d-6aa2-4757-9745-b85d35e3d326;IgnoreUnknown=1;K8S_POD_NAMESPACE=kube-system;K8S_POD_NAME=coredns-c676cc86f-4kz2t",
		Path:        "/opt/cni/bin",
		StdinData:   ([]byte)("{\"cniVersion\":\"0.3.0\",\"mode\":\"ipip\",\"name\":\"testcni\",\"subnet\":\"10.244.0.0\",\"type\":\"testcni\"}"),
	}
	var podIp = "10.10.10.10/32" // 内部ip32位掩码即可

	// containerVeth 设置IP, 并启动
	podAddr, err := netlink.ParseAddr(podIp)
	if err != nil {
		return err
	}

	vethContainerName := args.IfName

	// 获取 netns
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}

	var hostVeth *netlink.Veth

	// 创建容器veth对, Do函数的实现其实就是切到目标命名空间执行函数, 再切回来
	netns.Do(func(hostNS ns.NetNS) error {
		vethHostName, err := getVethNameOnHost()
		if err != nil {
			return err
		}
		err = netlink.LinkAdd(&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: vethContainerName,
				MTU:  1500,
			},
			PeerName: vethHostName,
		})
		if err != nil {
			return err
		}
		containerVeth, err := getVethByName(vethContainerName)
		if err != nil {
			return err
		}
		hostVeth, err = getVethByName(vethHostName)
		if err != nil {
			return err
		}

		// hostVeth移动到外边
		err = netlink.LinkSetNsFd(hostVeth, int(hostNS.Fd()))
		if err != nil {
			return err
		}

		err = netlink.AddrAdd(containerVeth, podAddr)
		if err != nil {
			return err
		}

		err = netlink.LinkSetUp(containerVeth) // 可以把netlink看作ip命令
		if err != nil {
			return err
		}

		/*
			路由score:
			  https://blog.csdn.net/u011673554/article/details/49125887
			  https://blog.51cto.com/u_847102/5226791?b=totalstatistic
			路由的转发显然,本地链路优先远端路由, 因为路由表可能有多条路径到达一个IP,但路由并不知道哪个更近,因此分层多种score, 近的score优先
				RT_SCOPE_LINK: 是本地链路的作用域。它包括所有直接连接到同一个物理网络的设备。
				RT_SCOPE_UNIVERSE是全局范围的作用域。它包括所有外部网络，如Internet。当数据包的目标地址不在本地链路上时
				RT_SCOPE_HOST是主机的作用域。它用于指示路由表中的特定条目只适用于本地主机。
		*/

		/*
			容器设置默认路由: 使用一个虚拟IP, 模仿 calico 使用 “169.254.1.1” (链路本地地址, IPv4链路本地地址定义在169.254.0.0/16地址块)
				当添加路由目的地址时, 会检查目标地址是否可达(是否在当前路由表的网段). 为了添加一个不可达的ip, 需要告诉路由表这个ip可达.
				root@vm:~# ip route add 10.20.0.0/16 via 10.251.10.127
				Error: Nexthop has invalid gateway.
				root@vm:~# ip route add 10.251.10.127 dev ens34
				root@vm:~# ip route add 10.20.0.0/16 via 10.251.10.127
				root@vm:~# route
				Kernel IP routing table
				Destination     Gateway         Genmask         Flags Metric Ref    Use Iface
				10.20.0.0       10.251.10.127   255.255.0.0     UG    0      0        0 ens34
				10.251.10.127   0.0.0.0         255.255.255.255 UH    0      0        0 ens34
		*/
		const DEFAULT_POST_GW = "169.254.1.1/32"

		// containerVeth 设置IP, 并启动
		gwIp, err := netlink.ParseAddr(DEFAULT_POST_GW)
		if err != nil {
			return err
		}

		err = netlink.RouteAdd(&netlink.Route{
			LinkIndex: containerVeth.Attrs().Index,
			Dst:       gwIp.IPNet,
		})
		if err != nil {
			return err
		}

		_, defaultDst, _ := net.ParseCIDR("0.0.0.0/0")
		err = netlink.RouteAdd(&netlink.Route{
			LinkIndex: containerVeth.Attrs().Index,
			Dst:       defaultDst,
			Gw:        gwIp.IP,
		})
		if err != nil {
			return err
		}

		return nil
	})

	/*
		任务:
			1. 启动主机的veth
			2. 打开/proc/sys/net/ipv4/conf/%s/proxy_arp, /proc/sys/net/ipv4/conf/%s/forwarding
			3. 创建路由到podIP
	*/

	err = netlink.LinkSetUp(hostVeth)
	if err != nil {
		return err
	}

	err = os.WriteFile(fmt.Sprintf("/proc/sys/net/ipv4/conf/%s/proxy_arp", hostVeth.Attrs().Name), []byte("1"), 0644)
	if err != nil {
		return err
	}
	err = os.WriteFile(fmt.Sprintf("/proc/sys/net/ipv4/conf/%s/forwarding", hostVeth.Attrs().Name), []byte("1"), 0644)
	if err != nil {
		return err
	}

	_, defaultDst, _ := net.ParseCIDR("0.0.0.0/0")
	err = netlink.RouteAdd(&netlink.Route{
		LinkIndex: hostVeth.Attrs().Index,
		Dst:       defaultDst,
		Gw:        podAddr.IP, // todo 怎么不需要绕过了?
	})
	if err != nil {
		return err
	}

}
