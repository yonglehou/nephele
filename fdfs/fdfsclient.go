package fdfs

import (
	//"errors"
	"fmt"
	cat "github.com/ctripcorp/cat.go"
	"github.com/ctripcorp/nephele/util"
	"math/rand"
	"strconv"
	"sync"
)

type FdfsClient interface {
	// download to buffer
	DownloadToBuffer(fileId string, catInstance cat.Cat) ([]byte, error)

	// upload by buffer
	UploadByBuffer(groupName string, filebuffer []byte, fileExtName string) (string, error)

	// upload slave by buffer
	UploadSlaveByBuffer(filebuffer []byte, remoteFileId string, prefixName string, fileExtName string) (string, error)

	// delete file
	DeleteFile(remoteFileId string) error
}

//cat instance transferred by user
//var userCat cat.Cat
var globalCat cat.Cat

func init() {
	util.InitCat()
	globalCat = cat.Instance()
}

type fdfsClient struct {
	//tracker client containing a connetction pool
	tracker *trackerClient

	//storage client map
	storages map[string]*storageClient

	//use to read or write a storage client from map
	mutex sync.RWMutex
}

//NewFdfsClient create a connection pool to a tracker
//the tracker is selected randomly from tracker group
func NewFdfsClient(trackerHosts []string, trackerPort string) (FdfsClient, error) {
	//select a random tracker host from host list
	host := trackerHosts[rand.Intn(len(trackerHosts))]
	port, err := strconv.Atoi(trackerPort)
	if err != nil {
		return nil, err
	}
	tc, err := newTrackerClient(host, port)
	if err != nil {
		return nil, err
	}
	return &fdfsClient{tracker: tc, storages: make(map[string]*storageClient)}, nil
}

func (this *fdfsClient) DownloadToBuffer(fileId string, catInstance cat.Cat) ([]byte, error) {
	//if catInstance == nil {
	//	return nil, errors.New("cat instance transferred to fdfs is nil")
	//}
	buff, err := this.downloadToBufferByOffset(fileId, 0, 0, catInstance)
	if err != nil {
		return nil, err
	}
	return buff, nil
}

func (this *fdfsClient) UploadByBuffer(groupName string, filebuffer []byte, fileExtName string) (string, error) {
	//query a upload server from tracker
	storeInfo, err := this.tracker.queryStroageStoreWithGroup(groupName)
	if err != nil {
		return "", err
	}
	//get a storage client from storage map, if not exist, create a new storage client
	storeClient, err := this.getStorage(storeInfo.ipAddr, storeInfo.port)
	if err != nil {
		return "", err
	}

	return storeClient.storageUploadByBuffer(storeInfo, filebuffer, fileExtName)
}

func (this *fdfsClient) UploadSlaveByBuffer(filebuffer []byte, remoteFileId string, prefixName string,
	fileExtName string) (string, error) {
	tmp, err := splitRemoteFileId(remoteFileId)
	if err != nil || len(tmp) != 2 {
		return "", err
	}
	groupName := tmp[0]
	remoteFilename := tmp[1]

	//query a upload server from tracker
	storeInfo, err := this.tracker.queryStroageStoreWithGroup(groupName)
	if err != nil {
		return "", err
	}

	//get a storage client from storage map, if not exist, create a new storage client
	storeClient, err := this.getStorage(storeInfo.ipAddr, storeInfo.port)
	if err != nil {
		return "", err
	}

	return storeClient.storageUploadSlaveByBuffer(storeInfo, filebuffer, remoteFilename, prefixName, fileExtName)
}

func (this *fdfsClient) DeleteFile(remoteFileId string) error {
	tmp, err := splitRemoteFileId(remoteFileId)
	if err != nil || len(tmp) != 2 {
		return err
	}
	groupName := tmp[0]
	remoteFilename := tmp[1]

	storeInfo, err := this.tracker.trackerQueryStorageUpdate(groupName, remoteFilename)
	if err != nil {
		return err
	}

	//get a storage client from storage map, if not exist, create a new storage client
	storeClient, err := this.getStorage(storeInfo.ipAddr, storeInfo.port)
	if err != nil {
		return err
	}

	return storeClient.storageDeleteFile(storeInfo, remoteFilename)
}

func (this *fdfsClient) downloadToBufferByOffset(fileId string, offset int64, downloadSize int64, catInstance cat.Cat) ([]byte, error) {
	//split file id to two parts: group name and file name
	tmp, err := splitRemoteFileId(fileId)
	if err != nil || len(tmp) != 2 {
		return nil, err
	}
	groupName := tmp[0]
	fileName := tmp[1]

	//query a download server from tracker
	storeInfo, err := this.tracker.trackerQueryStorageFetch(groupName, fileName)
	if err != nil {
		return nil, err
	}
	if catInstance != nil {
		event := catInstance.NewEvent("ImgFromStorage", fmt.Sprintf("%s:%s", storeInfo.groupName, storeInfo.ipAddr))
		event.SetStatus("0")
		event.Complete()
	}

	//get a storage client from storage map, if not exist, create a new storage client
	storeClient, err := this.getStorage(storeInfo.ipAddr, storeInfo.port)
	if err != nil {
		return nil, err
	}
	return storeClient.storageDownload(storeInfo, offset, downloadSize, fileName)
}

func (this *fdfsClient) getStorage(ip string, port int) (*storageClient, error) {
	storageKey := fmt.Sprintf("%s-%d", ip, port)
	//if the storage with the key exists, return the stroage
	//else create a new stroage and return
	if sc := this.queryStorage(storageKey); sc != nil {
		return sc, nil
	} else {
		this.mutex.Lock()
		defer this.mutex.Unlock()
		//reconfirm wheather the storage exists
		if sc, ok := this.storages[storageKey]; ok {
			return sc, nil
		} else {
			sc, err := newStorageClient(ip, port)
			if err != nil {
				return nil, err
			}
			this.storages[storageKey] = sc
			return sc, nil
		}
	}
}

//query a storage client from storage map by key
//if the storage not eixst, return nil
func (this *fdfsClient) queryStorage(key string) *storageClient {
	this.mutex.RLock()
	defer this.mutex.RUnlock()
	if sc, ok := this.storages[key]; ok {
		return sc
	} else {
		return nil
	}
}
