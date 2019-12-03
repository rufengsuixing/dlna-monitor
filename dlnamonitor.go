package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"golang.org/x/net/ipv4"
)

type device struct {
	ipport  string
	timeout int
}

func udpMuticastListener(resub chan map[string]int) {
	//1. 得到一个interface
	//var interfacename string
	//fmt.Scanln(&interfacename)
	en4, err := net.InterfaceByName("br-lan")
	if err != nil {
		fmt.Println(err)
	}
	group := net.IPv4(239, 255, 255, 250)
	//2. bind一个本地地址
	c, err := net.ListenPacket("udp4", "239.255.255.250:1900")
	if err != nil {
		fmt.Println(err)
	}
	defer c.Close()
	//3.
	p := ipv4.NewPacketConn(c)
	if err := p.JoinGroup(en4, &net.UDPAddr{IP: group}); err != nil {
		fmt.Println(err)
	}
	//4.更多的控制
	if err := p.SetControlMessage(ipv4.FlagDst, true); err != nil {
		fmt.Println(err)
	}
	//5.接收消息
	fmt.Println("start to listen muticast")
	var spos, epos int
	b := make([]byte, 1500)
	devices := make(map[string]int)
	var s string
	shout := make(chan *device)
	var mindevice string
	var min = 60
	var timeout *time.Timer
		go func() {
			for {
			one := device{}
			//println("read")
			n, cm, _, err := p.ReadFrom(b)
			//println("readok")
			if err != nil {
				fmt.Println(err)
			}
			if cm.Dst.IsMulticast() {
				if cm.Dst.Equal(group) {
					s = string(b[:n])
					spos = strings.Index(s, "CACHE-CONTROL: max-age=")
					if spos == -1 {
						continue
					}
					spos += 23
					epos = strings.Index(s[spos:], "\n") - 1 + spos
					age, _ := strconv.Atoi(s[spos:epos])

					spos = strings.Index(s[epos:], "LOCATION: http://") + epos
					if spos == -1 {
						continue
					}
					spos += 17
					//mpos = strings.Index(s[spos:], ":") + spos
					epos = strings.Index(s[spos:], "/description.xml") + spos
					one.ipport = s[spos:epos]
					one.timeout = age
					//println("get", s[spos:epos], age)
					shout <- &one
					//print(".")
					//println("sended", s[spos:epos], age)
				} else {
					fmt.Println("Unknown group")
					return
				}
			}else{
				println("not muticast")
				return
			}}
		}()
		for {
		timestamp := time.Now().Unix()
		resubflag := false
		var one *device
		timeout = time.NewTimer(time.Duration(min)*time.Second)
		if len(devices) == 0{
			timeout.Stop()
		}
		select {
		case one = <-shout:
			timeout.Stop()
			min = one.timeout
			if len(devices) == 0 {
				resubflag = true
				devices[one.ipport] = one.timeout
				mindevice=one.ipport
			}else{
				devices[one.ipport] = one.timeout
				timegap := int((time.Now().Unix() - timestamp) / int64(time.Second))
				//println(timegap)
				for o := range devices {
					if o == one.ipport {
						continue
					}
					println(timegap)
					devices[o] -= timegap
					if devices[o] < 0 {
						delete(devices, o)
						resubflag = true
						println("del")
					}else if min > devices[o] {
						min = devices[o]
						mindevice=o
					}
				}
			}	
			if resubflag {
				println("resub")
				resub <- devices
			}
		case <-timeout.C: 
			println(mindevice,"device timeout")
			delete(devices, mindevice)
			timegap := int((time.Now().Unix() - timestamp) / int64(time.Second))
			min=600
			for o := range devices {
				devices[o] -= timegap
				if devices[o] < 0 {
					delete(devices, o)
				}else if min > devices[o] {
					min = devices[o]
					mindevice =o 
				}
				}
			resub <- devices
			//println("resub")
		}
	}
}

func tcpdump(resub chan map[string]int) {
	var d map[string]int
	running := false
	killprint := make(chan bool)
	fmt.Println("tcpdump waiting for massage")
	for {
		d = <-resub
		var addstrlist []string
		for a := range d {
			pos := strings.Index(a, ":")
			addstrlist = append(addstrlist, fmt.Sprintf("(dst %s and port %s)", a[:pos], a[pos+1:]))
		}
		if running {
			println("kill tcpdump")
			killprint <- true
		}
		if len(d) != 0 {
			println(fmt.Sprintf("tcpdump -n tcp and (%s)", strings.Join(addstrlist, " or ")))
			go Run(killprint, "tcpdump","-n",fmt.Sprintf("tcp and (%s)", strings.Join(addstrlist, " or ")))
			running = true
		} else {
			running = false
		}
	}
}
func Run(killprint chan bool, name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	// 命令的错误输出和标准输出都连接到同一个管道
	stdout, err := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout

	if err != nil {
		return err
	}

	if err = cmd.Start(); err != nil {
		return err
	}
	// 从管道中实时获取输出并打印到终端
	go func() {
		var s string = ""
		for {
			tmp := make([]byte, 1024)
			_, err := stdout.Read(tmp)
			s += string(tmp)
			spos := strings.Index(s, "\n")
			if spos == -1 {
				continue
			}
			epos := strings.Index(s, "Flags")
			if epos == -1 {
				continue
			}
			if (epos-spos>2){
			fmt.Println(s[spos+1 : epos-1])
			}
			s = s[epos+5:]
			if err != nil {
				break
			}
		}
	}()

//	if err = cmd.Wait(); err != nil {
//		return err
//	}
	println("wait to kill tcpdump")
	<-killprint
	println("true kill")
	cmd.Process.Kill()
	cmd.Wait()
	return nil
}

func main() {
	resub := make(chan map[string]int)
	go udpMuticastListener(resub)
	go tcpdump(resub)

	sigs := make(chan os.Signal, 1)
	done := make(chan bool)
	signal.Notify(sigs, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		for range sigs {
			fmt.Println("stopping...")
			done <- true
		}
	}()
	<-done
}
