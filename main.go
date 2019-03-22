package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	hostIP         string // 代理主机ip
	port           int    // 代理主机端口
	isOpen         bool   // 是否公开访问，true表示所有网络都可以访问，false表示只有本机访问
	podName        string // 转发到pod的名称，需要包含代理类型名称
	targetPort     int    // 转发到app所在的端口
	delayTime      int    // 定时访问时间，单位：秒
	httpProtocol   string // http协议，http或https
	namespace      string // k8s的名称空间
	serviceAccount string // serviceAccount的名字
)

func init() {
	flag.StringVar(&hostIP, "hostIP", "", "Proxy host IP")
	flag.IntVar(&port, "port", -1, "Proxy Host Port")
	flag.BoolVar(&isOpen, "isOpen", false, "Is it publicly accessible?")
	flag.StringVar(&podName, "podName", "", "Forwarding port to pod name")
	flag.IntVar(&targetPort, "targetPort", -1, "Forwarding to the port where the app is located")
	flag.StringVar(&namespace, "namespace", "default", "http Protocol")
	flag.StringVar(&serviceAccount, "serviceAccount", "", "k8s service account name")
	flag.IntVar(&delayTime, "delayTime", 240, "Access interval time")
	flag.StringVar(&httpProtocol, "httpProtocol", "", "http Protocol, http or https")
	flag.Parse()

	if hostIP == "" || port == -1 || podName == "" || targetPort == -1 {
		fmt.Println(`
parameter error.

some examples:

(1) kibana proxy
    kproxy --hostIP=192.168.8.100 --port=8080 --podName=nginx-to-kibana --targetPort=80 --namespace=default --httpProtocol=http

(2) dashboard proxy
    kproxy --hostIP=192.168.8.100 --port=8081 --podName=kubernetes-dashboard --targetPort=8443 --namespace=kube-system --httpProtocol=https --serviceAccount=dashboard-admin

(3) grafana proxy: 
    kproxy --hostIP=192.168.8.100 --port=8082 --podName=monitoring-grafana --targetPort=3000 --namespace=prom --httpProtocol=http

spec: 
    if you need publicly accessible, add parameter '--isOpen=true'
`)

		os.Exit(1)
	}
}

func main() {
	// 定期访问
	go func() {
		if !isOpen {
			hostIP = "localhost"
			fmt.Printf("\nOnly allowed to access localhost\n")
		}

		url := fmt.Sprintf("%s://%s:%d", httpProtocol, hostIP, port)
		if strings.Contains(podName, "kibana") {
			url += "/_plugin/kibana"
		}
		fmt.Printf("\naccess addr is %s\n\n", url)

		for {
			time.Sleep(time.Duration(delayTime) * time.Second)
			err := httpGet(url)
			if err != nil {
				fmt.Printf("http get error, url=%s, err=%s\n", url, err.Error())
			}
		}
	}()

	// 如果是dashboard转发，获取token值
	if strings.Contains(podName, "dashboard") {
		command := fmt.Sprintf("kubectl describe secret $(kubectl get secret -n %s | grep %s-token | awk '{print $1}') -n %s | awk '/^token:/{print $2}'",
			namespace, serviceAccount, namespace)
		result, err := ExecShellCMD(command)
		if err != nil {
			fmt.Printf("execute cmd error, command=%s, err=%s\n", command, err.Error())
			return
		}
		fmt.Printf("\n%s\n\n", result)
	}

	openAddr := ""
	if isOpen {
		openAddr += "--address 0.0.0.0"
	}

	// 执行端口转发命令
	command := fmt.Sprintf(`kubectl port-forward %s $(kubectl get pods -n %s | grep %s | awk '{print $1}') %d:%d -n %s`,
		openAddr, namespace, podName, port, targetPort, namespace)
	message := make(chan string)
	go ExecBlockShellCMD(command, message)
	for val := range message {
		fmt.Printf("%s %s", time.Now().Format("2006-01-02 15:04:05"), val)
		// 端口转发失败
		if strings.Contains(val, "Unable to listen") {
			break
		}
	}

	fmt.Println("process exit")
}

// 安静的访问
func httpGet(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return nil
}

// 执行阻塞的shell命令，执行结果返回在channel中
func ExecBlockShellCMD(command string, message chan string) {
	defer func() { close(message) }()

	cmd := exec.Command("/bin/sh", "-c", command)
	fmt.Println(cmd.Args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		message <- fmt.Sprintf("stdout error, err = %s\n", err.Error())
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		message <- fmt.Sprintf("stderr error, err = %s\n", err.Error())
		return
	}

	err = cmd.Start()
	if err != nil {
		message <- fmt.Sprintf("cmd start error, err = %s\n", err.Error())
		return
	}

	reader := bufio.NewReader(stdout)
	// 读取内容
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			message <- fmt.Sprintf("stdout error, err = %s\n", err.Error())
			break
		}
		message <- line
	}

	err = cmd.Wait()
	if err != nil {
		message <- fmt.Sprintf("cmd wait error, err = %s\n", err.Error())
	}

	// 命令错误处理
	bytesErr, err := ioutil.ReadAll(stderr)
	if err != nil {
		message <- fmt.Sprintf("read stderr error, err = %s\n", err.Error())
		return
	}
	if len(bytesErr) != 0 {
		message <- string(bytesErr)
		return
	}

	message <- "command exec finish"
}

// 执行非阻塞的shell命令
func ExecShellCMD(command string) (string, error) {
	cmd := exec.Command("/bin/sh", "-c", command)
	fmt.Println(cmd.Args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	bytesErr, err := ioutil.ReadAll(stderr)
	if err != nil {
		return "", err
	}

	if len(bytesErr) != 0 {
		return "", errors.New(string(bytesErr))
	}

	bytes, err := ioutil.ReadAll(stdout)
	if err != nil {
		return "", err
	}

	if err := cmd.Wait(); err != nil {
		return "", err
	}

	return string(bytes), nil
}

// 去掉换行符
func getData(data []byte) string {
	l := len(data)
	if l > 0 {
		return string(data[:l-1])
	}

	return ""
}
