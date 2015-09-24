package models

import (
	"fmt"
	"github.com/astaxie/beego/orm"
	cat "github.com/ctripcorp/cat.go"
	"github.com/ctripcorp/nephele/util"
	"time"
)

type Channel struct {
	Name string
	Code string
	Cat  cat.Cat
}

var (
	getChannelsTime time.Time         = time.Now()
	channels        map[string]string = make(map[string]string)

	ERRORTYPE_GETCHANNEL = "GetChannel"
)

func (this *Channel) Insert() error {
	var err error
	if this.Cat != nil {
		tran := this.Cat.NewTransaction(DBTITLE, "Channel.Insert")
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
	_, err = o.Raw("INSERT INTO channel(name,code) VALUES(?,?)", this.Name, this.Code).Exec()
	return err
}

func (this *Channel) Update() error {
	var err error
	if this.Cat != nil {
		tran := this.Cat.NewTransaction(DBTITLE, "Channel.Upldate")
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
	_, err = o.Raw("UPDATE channel SET code=? WHERE name=?", this.Code, this.Name).Exec()
	return err
}

func (this *Channel) GetChannels() (map[string]string, util.Error) {
	var err error
	if len(channels) < 1 || IsRefresh(getChannelsTime) {
		if this.Cat != nil {
			var err error
			tran := this.Cat.NewTransaction(DBTITLE, "Channel.Get")
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
		res := make(orm.Params)
		_, err = o.Raw("SELECT name,code FROM channel").RowsToMap(&res, "name", "code")
		if err != nil {
			return nil, util.Error{IsNormal: false, Err: err, Type: ERRORTYPE_GETCHANNEL}
		}
		m := make(map[string]string)
		for k, v := range res {
			m[k] = fmt.Sprintf("%v", v)
		}
		channels = m
		getChannelsTime = time.Now()
	}
	return channels, util.Error{}
}

func (this *Channel) GetChannelCode(channel string) string {
	channels, e := this.GetChannels()
	if e.Err != nil {
		return ""
	}
	code, _ := channels[channel]
	return code
}

func IsRefresh(t time.Time) bool {
	return t.Add(1 * time.Minute).Before(time.Now())
}
