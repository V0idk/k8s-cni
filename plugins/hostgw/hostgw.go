package hostgw

/*
主机路由形式: 每个节点手工规划了分配区域. 比如:
A: 10.200.1.0/24
B: 10.200.2.0/24

! 节点内通信： bridge的veth对即可。
bridge <-----> veth1 <-------> pod(veth)
! 节点间通信: 手工加路由. 手工给各个节点的bridge分配的网段添加路由.

*/

type HostGatewayCNI struct {
}
