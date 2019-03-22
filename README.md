# kproxy
k8s port forward to local, dependent on [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)-1.12+.

<br>

### usage

```bash
go get -u https://github.com/zhufuyi/kproxy.git 
cd $src/github.com/zhufuyi/kproxy
go build
kproxy --hostIP=192.168.8.100 --port=8080 --podName=nginx-to-kibana --targetPort=80 --httpProtocol=http --isOpen=true
```
