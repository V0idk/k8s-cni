package ipip

import (
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

/*

同节点通信

*/

type IpipCNI struct{}

func (ipip *IpipCNI) Add(args *skel.CmdArgs) error {
	args = &skel.CmdArgs{
		ContainerID: "308102901b7fe9538fcfc71669d505bc09f9def5eb05adeddb73a948bb4b2c8b",
		Netns:       "/var/run/netns/ns3",
		IfName:      "eth0",
		Args:        "K8S_POD_INFRA_CONTAINER_ID=308102901b7fe9538fcfc71669d505bc09f9def5eb05adeddb73a948bb4b2c8b;K8S_POD_UID=d392609d-6aa2-4757-9745-b85d35e3d326;IgnoreUnknown=1;K8S_POD_NAMESPACE=kube-system;K8S_POD_NAME=coredns-c676cc86f-4kz2t",
		Path:        "/opt/cni/bin",
		StdinData:   ([]byte)("{\"cniVersion\":\"0.3.0\",\"mode\":\"ipip\",\"name\":\"testcni\",\"subnet\":\"10.244.0.0\",\"type\":\"testcni\"}"),
	}
	var podIp string
	ifName := args.IfName

	// 获取 netns
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return err
	}

	var containerVeth, hostVeth *netlink.Veth
	// 进入新创建的命名空间做以下操作:

	err = netns.Do(func(hostNs ns.NetNS) error {
		var vethPairName = "veth随机不冲突"

		// 创建 veth pair: `ip link add $link`
		err := netlink.LinkAdd(&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: ifName,
				MTU:  1500,
			},
			PeerName: vethPairName,
		})

		veth1, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}
		containerVeth = veth1.(*netlink.Veth)

		veth2, err := netlink.LinkByName(vethPairName)
		if err != nil {
			return err
		}
		hostVeth = veth2.(*netlink.Veth)

		// 把随机起名的 veth 那头放在主机上
		err = netlink.LinkSetNsFd(hostVeth, int(hostNs.Fd()))
		if err != nil {
			return err
		}

		// 然后把要被放到 pod 中的那头 veth 塞上 podIP
		err = netlink.LinkSetNsFd(containerVeth.Name, podIP)
		if err != nil {
			utils.WriteLog("给 veth 设置 ip 失败, err: ", err.Error())
			return err
		}

		// 然后启动它
		err = nettools.SetUpVeth(containerVeth)
		if err != nil {
			utils.WriteLog("启动 veth pair 失败, err: ", err.Error())
			return err
		}

		return setFibTalbeIntoNs(DEFAULT_POST_GW, containerVeth)
	})
	if err != nil {
		return err
	}

}
