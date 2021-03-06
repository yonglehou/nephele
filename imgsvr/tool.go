package imgsvr

import (
	"bytes"
	"errors"
	log "github.com/Sirupsen/logrus"
	cat "github.com/ctripcorp/cat.go"
	"github.com/ctripcorp/nephele/imgsvr/data"
	"github.com/ctripcorp/nephele/imgsvr/img4g"
	"github.com/ctripcorp/nephele/imgsvr/proc"
	"github.com/ctripcorp/nephele/imgsvr/storage"
	"github.com/ctripcorp/nephele/util"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	Image       = "Image"
	Hotel       = "hotel"
	Globalhotel = "globalhotel"
	TG          = "tg"
	Reboot      = "Reboot"
	CatInstance cat.Cat
	fdfsUrl     = util.RegexpExt{regexp.MustCompile("fd/([a-zA-Z]+)/(.*)")}
	nfs1Url     = util.RegexpExt{regexp.MustCompile("t1/([a-zA-Z]+)/(.*)")}
	nfs2Url     = util.RegexpExt{regexp.MustCompile("([a-zA-Z]+)/(.*)")}

	fd         = "fd"
	nfs1       = "nfs1"
	nfs2       = "nfs2"
	nfs        = "nfs"
	WorkerPort string
)

func init() {
	util.InitCat()
	CatInstance = cat.Instance()
}

//var StartPort int
type nepheleTask struct {
	inImg *img4g.Image
	chain *proc.ProcessorChain
	//response chan
	rspChan chan bool

	CatInstance cat.Cat

	//if true, the task will be canceled
	canceled bool

	//use to read or set canceled
	mutex sync.Mutex
}

func (nt *nepheleTask) SetCanceled() {
	nt.mutex.Lock()
	defer nt.mutex.Unlock()
	nt.canceled = true
}

func (nt *nepheleTask) GetCanceled() bool {
	nt.mutex.Lock()
	defer nt.mutex.Unlock()
	return nt.canceled
}

//chan containing tasks waiting to be processed
var taskChan = make(chan *nepheleTask, 1000)

//sourceType, channel, path
func ParseUri(path string) (string, string, string) {
	var sourceType = fd
	params, ok := fdfsUrl.FindStringSubmatchMap(path)
	if !ok {
		sourceType = nfs1
		params, ok = nfs1Url.FindStringSubmatchMap(path)
		if !ok {
			sourceType = nfs2
			params, ok = nfs2Url.FindStringSubmatchMap(path)
			if !ok {
				sourceType = ""
			}
		}
	}

	channel := params[":1"]
	p := params[":2"]
	switch sourceType {
	case fd:
		return "FastDFS", strings.ToLower(channel), p
	case nfs1:
		return "NFS", strings.ToLower(channel), getTargetDir(channel, nfs1) + channel + "/" + p
	case nfs2:
		return "NFS", strings.ToLower(channel), getTargetDir(channel, nfs2) + channel + "/" + p
	}
	return "FastDFS", "", ""
}

func getTargetDir(channel, storagetype string) string {
	dir, _ := data.GetDirPath(channel, storagetype)
	return dir
}

func JoinString(args ...string) string {
	var buf bytes.Buffer
	for _, v := range args {
		buf.WriteString(v)
	}
	return buf.String()
}

func GetStorage(storageType string, path string, Cat cat.Cat) (storage.Storage, error) {
	var srg storage.Storage
	switch storageType {
	case "FastDFS":
		domain, err := data.GetFdfsDomain()
		if err != nil {
			return nil, err
		}
		port := data.GetFdfsPort()
		srg = &storage.Fdfs{
			Path:          path,
			TrackerDomain: domain,
			Port:          port,
			Cat:           Cat,
		}
	case "NFS":
		srg = &storage.Nfs{path}
	}
	if srg == nil {
		return nil, errors.New("storageType(" + storageType + ") isn't supported!")
	}
	return srg, nil
}
func GetImage(storageType string, path string, Cat cat.Cat) ([]byte, error) {
	srg, err := GetStorage(storageType, path, Cat)
	if err != nil {
		return nil, err
	}
	return srg.GetImage()
}

var localIP string = ""

func GetIP() string {
	if localIP != "" {
		return localIP
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		add := strings.Split(addr.String(), "/")[0]
		if add == "127.0.0.1" || add == "::1" {
			continue
		}
		first := strings.Split(add, ".")[0]
		if _, err := strconv.Atoi(first); err == nil {
			localIP = add
			return add
		}
	}
	return ""
}

func GetHttp(url string) ([]byte, error) {
	timeout := time.Duration(time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	rsp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	bts, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return nil, err
	}
	return bts, nil
}

func PostHttp(uri string, data url.Values) ([]byte, error) {
	timeout := time.Duration(time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	rsp, err := client.PostForm(uri, data)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	bts, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return nil, err
	}
	return bts, nil
}

func KillProcessByPort(port string) error {
	cmd := exec.Command("sh", "-c", JoinString("lsof -i:", port, "|grep LISTEN|awk '{print $2}'"))
	//	cmd.Env = append(cmd.Env, os.Getenv("PATH"))
	bts, err := cmd.Output()
	if err != nil {
		return err
	}
	pid := strings.TrimSpace(string(bts))
	if pid == "" {
		return nil
	}
	id, err := strconv.Atoi(pid)
	if err != nil {
		return err
	}
	p, err := os.FindProcess(id)
	if err != nil {
		return err
	}
	return p.Kill()
}

func GetImageSizeDistribution(size int) string {
	switch {
	case size < 0:
		return "<0"
	case size == 0:
		return "0"
	case size > 0 && size <= 512*1024:
		return "1~512KB"
	case size > 512*1024 && size <= 1024*1024:
		return "512~1024KB"
	case size > 1024*1024 && size <= 2*1024*1024:
		return "1~2M"
	case size > 2*1024*1024 && size <= 4*1024*1024:
		return "2~4M"
	case size > 4*1024*1024 && size <= 6*1024*1024:
		return "4~6M"
	case size > 6*1024*1024 && size <= 10*1024*1024:
		return "6~10M"
	case size > 10*1024*1024 && size <= 20*1024*1024:
		return "10~20M"
	case size > 20*1024*1024 && size <= 30*1024*1024:
		return "20~30M"
	default:
		return ">30M"
	}
}

func LogErrorEvent(cat cat.Cat, name string, err string) {
	event := cat.NewEvent("Error", name)
	event.AddData("detail", err)
	event.SetStatus("ERROR")
	event.Complete()
}

func LogEvent(cat cat.Cat, title string, name string, data map[string]string) {
	event := cat.NewEvent(title, name)
	if data != nil {
		for k, v := range data {
			event.AddData(k, v)
		}
	}
	event.SetStatus("0")
	event.Complete()
}

//log error with logging fields uri
func logErrWithUri(uri string, errMsg string, errLevel string) {
	switch errLevel {
	case "errorLevel":
		log.WithFields(log.Fields{
			"uri": uri,
		}).Error(errMsg)
	default:
		log.WithFields(log.Fields{
			"uri": uri,
		}).Warn(errMsg)
	}
}
