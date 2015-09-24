package models

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/astaxie/beego/orm"
	cat "github.com/ctripcorp/cat.go"
	"github.com/ctripcorp/nephele/util"
	"strconv"
	"strings"
	"time"
)

var (
	ERRTYPE_IMAGENAMEINVALID   = "ImageNameInvalid"
	ERRTYPE_GETIMAGEINDEX      = "GetImageIndex"
	ERRORTYPE_INSERTIMAGEINDEX = "InsertImageIndex"
	ERRORTYPE_INSERTIMAGEPLAN  = "InsertImagePlan"
	ERRORTYPE_DELETEIMAGEINDEX = "DeleleImageIndex"
	DBTITLE                    = "ImageWS.DB"
	NEWIMAGENAMELENGTH         = 21
	DEFAULTVERSION             = "0"
)

type ImageIndex struct {
	Idx          int64
	Channel      string
	StoragePath  string
	StorageType  string
	Profile      string
	PartitionKey int16
	TableZone    int
	Version      string
	Cat          cat.Cat
}

func getDBString(tableZone int) string {
	return "default"
}
func getExt(path string) string {
	arr := strings.Split(path, ".")
	return arr[len(arr)-1]
}

func (this *ImageIndex) SaveToDB(plan string) util.Error {
	var (
		res sql.Result
		err error
		id  int64
	)
	if this.Cat != nil {
		tran := this.Cat.NewTransaction(DBTITLE, "ImageIndex.Insert")
		defer func() {
			if err != nil {
				tran.SetStatus(err)
			} else {
				tran.SetStatus("0")
			}
			tran.Complete()
		}()
	}

	o := orm.NewOrm()
	o.Using(getDBString(this.TableZone))
	o.Begin()
	partitionKey := util.GetPartitionKey(time.Now())
	res, err = o.Raw("INSERT INTO `imageindex_"+strconv.Itoa(this.TableZone)+"` (`channel`,`storagePath`,`storageType`,`profile`,`createtime`,`partitionKey`)VALUES(?,?,?,?,NOW(),?)", this.Channel, this.StoragePath, this.StorageType, this.Profile, partitionKey).Exec()

	if err != nil {
		o.Rollback()
		return util.Error{IsNormal: false, Err: err, Type: ERRORTYPE_INSERTIMAGEINDEX}
	}
	id, err = res.LastInsertId()
	if err != nil {
		o.Rollback()
		return util.Error{IsNormal: false, Err: err, Type: ERRORTYPE_INSERTIMAGEINDEX}
	}
	if plan != "" {
		if this.Cat != nil {
			tran := this.Cat.NewTransaction(DBTITLE, "ImagePlan.Insert")
			defer func() {
				if err != nil {
					tran.SetStatus(err)
				} else {
					tran.SetStatus("0")
				}
				tran.Complete()
			}()
		}
		res, err = o.Raw("INSERT INTO `imageplan_"+strconv.Itoa(this.TableZone)+"`(imgIdx,plan,partitionKey)VALUES(?,?,?)", id, plan, partitionKey).Exec()
		if err != nil {
			o.Rollback()
			return util.Error{IsNormal: false, Err: err, Type: ERRORTYPE_INSERTIMAGEPLAN}
		}
		if _, err = res.RowsAffected(); err != nil {
			o.Rollback()
			return util.Error{IsNormal: false, Err: err, Type: ERRORTYPE_INSERTIMAGEPLAN}
		}
	}
	if err = o.Commit(); err != nil {
		return util.Error{IsNormal: false, Err: err, Type: ERRORTYPE_INSERTIMAGEINDEX}
	}
	this.Idx = id
	this.PartitionKey = partitionKey
	return util.Error{}
}

func (this ImageIndex) GetImageName() string {
	ext := getExt(this.StoragePath)                              //扩展名
	zone := strconv.FormatInt(int64(this.TableZone), 36)         //转36进制
	partition := strconv.FormatInt(int64(this.PartitionKey), 36) //转36进制
	//1~2 频道 3~4 分区 5~6 时间 7 版本号 8~17 索引 18~21 检验码
	tmp := util.JoinString(this.Channel, util.Cover(zone, "0", 2), util.Cover(partition, "0", 2), DEFAULTVERSION, util.Cover(strconv.FormatInt(this.Idx, 10), "0", 10))
	return util.JoinString("\\", tmp, util.Compute(tmp), ".", ext)
}

func (this *ImageIndex) Parse(imageName string) util.Error {
	if e := this.ParseName(imageName); e.Err != nil {
		return e
	}
	if e := this.GetStorage(); e.Err != nil {
		return e
	}
	return util.Error{}
}

func (this *ImageIndex) DropExtension(imageName string) string {
	imageName = strings.Replace(imageName, "\\", "", -1)
	imageName = strings.Replace(imageName, "/", "", -1)
	return strings.Split(imageName, ".")[0]
}

func (this *ImageIndex) ParseName(imgName string) util.Error {
	imageName := this.DropExtension(imgName)
	if len(imageName) != NEWIMAGENAMELENGTH {
		return util.Error{IsNormal: false, Err: errors.New("imagename length is invalid"), Type: ERRTYPE_IMAGENAMEINVALID}
	}
	swithoutcompute := util.Substr(imageName, 0, NEWIMAGENAMELENGTH-4)
	scompute := util.Substr(imageName, NEWIMAGENAMELENGTH-4, 4)
	if util.Compute(swithoutcompute) != scompute {
		return util.Error{IsNormal: true, Err: errors.New("Compute check faile."), Type: ERRTYPE_IMAGENAMEINVALID}
	}
	channel := util.Substr(imageName, 0, 2)

	tableZone, err := strconv.ParseInt(util.Substr(imageName, 2, 2), 36, 10)
	if err != nil {
		return util.Error{IsNormal: true, Err: errors.New("tablezone is invalid."), Type: ERRTYPE_IMAGENAMEINVALID}
	}
	partitionKey, err := strconv.ParseInt(util.Substr(imageName, 4, 2), 36, 10)
	if err != nil {
		return util.Error{IsNormal: true, Err: errors.New("partition is invalid."), Type: ERRTYPE_IMAGENAMEINVALID}
	}
	version := util.Substr(imageName, 6, 1)
	idx, err := strconv.ParseInt(util.Substr(imageName, 7, 10), 10, 10)
	if err != nil {
		return util.Error{IsNormal: true, Err: errors.New("index is invalid."), Type: ERRTYPE_IMAGENAMEINVALID}
	}
	this.Channel = channel
	this.Idx = idx
	this.PartitionKey = int16(partitionKey)
	this.TableZone = int(tableZone)
	this.Version = version
	return util.Error{}
}

func (this *ImageIndex) Delete() util.Error {
	var err error
	if this.Cat != nil {
		tran := this.Cat.NewTransaction(DBTITLE, "ImageIndex.Delete")
		defer func() {
			if err != nil {
				tran.SetStatus(err)
			} else {
				tran.SetStatus("0")
			}
			tran.Complete()
		}()
	}
	o := orm.NewOrm()
	o.Using(getDBString(this.TableZone))
	_, err = o.Raw("DELETE FROM `imageindex_"+strconv.Itoa(this.TableZone)+"` WHERE idx = ? AND partitionKey = ?", this.Idx, this.PartitionKey).Exec()
	if err != nil {
		return util.Error{IsNormal: false, Err: err, Type: ERRORTYPE_DELETEIMAGEINDEX}
	}
	return util.Error{}
}

func (this *ImageIndex) GetStorage() util.Error {
	if this.Idx < 1 || this.TableZone < 1 || this.PartitionKey < 1 {
		return util.Error{IsNormal: true, Err: errors.New("getimageindex parameters is invalid"), Type: ERRTYPE_GETIMAGEINDEX}
	}
	var result util.Error = util.Error{}
	if this.Cat != nil {
		tran := this.Cat.NewTransaction(DBTITLE, "ImageIndex.GetStorage")
		defer func() {
			if result.Err != nil {
				tran.SetStatus(result.Err)
			} else {
				tran.SetStatus("0")
			}
			tran.AddData("TabeZone", strconv.Itoa(this.TableZone))
			tran.AddData("Idx", strconv.FormatInt(this.Idx, 10))
			tran.AddData("PartitionKey", strconv.Itoa(int(this.PartitionKey)))
			tran.Complete()
		}()
	}

	o := orm.NewOrm()
	o.Using(getDBString(this.TableZone))
	res := make(orm.Params)
	nums, err := o.Raw(util.JoinString("SELECT storagepath,storagetype FROM imageindex_", strconv.Itoa(this.TableZone), " WHERE idx=? AND partitionKey=?"), this.Idx, this.PartitionKey).RowsToMap(&res, "storagepath", "storagetype")
	if err != nil {
		result = util.Error{IsNormal: false, Err: err, Type: ERRTYPE_GETIMAGEINDEX}
		return result
	}
	if nums < 1 {
		result = util.Error{IsNormal: false, Err: errors.New("idx is't exists"), Type: ERRTYPE_GETIMAGEINDEX}
		return result
	}
	for k, v := range res {
		this.StoragePath = k
		this.StorageType = fmt.Sprintf("%v", v)
	}
	return result
}
