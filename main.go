package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/toolkits/file"
)

var config *GlobalConfig

type GlobalConfig struct {
	Server   string `json:"server"`
	UserInfo []User `json:"user_info"`
	Mail     Mail   `json:"mail"`
}

type User struct {
	Mobile string `json:"mobile"`
	Code   string `json:"code"`
}

type Mail struct {
	SMTPSmarthost    string   `json:"smtp_smarthost"`
	SMTPFrom         string   `json:"smtp_from"`
	SMTPAuthUsername string   `json:"smtp_auth_username"`
	SMTPAuthIdentity string   `json:"smtp_auth_identity"`
	SMTPAuthPassword string   `json:"smtp_auth_password"`
	SMTPRequireTLS   bool     `json:"smtp_require_tls"`
	SmtpTo           []string `json:"smtp_to"`
}

type ISZCloudResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    struct {
		CityNo    string `json:"cityNo"`
		Address   string `json:"address"`
		Mobile    string `json:"mobile"`
		Name      string `json:"name"`
		ApplyTime string `json:"applyTime"`
		Status    string `json:"status"`

		WinTime    string `json:"winTime"`
		ExpireTime string `json:"expireTime"`
		SendNo     string `json:"sendNo"`
	} `json:"data"`
	Success bool `json:"success"`
}

func (this ISZCloudResp) String() string {
	if this.Success && (strings.Contains(this.Data.Status, "PAYED") || strings.Contains(this.Data.Status, "SUCC")) {
		return fmt.Sprintf("\n[中奖通知]恭喜你抽中奖了\n %v %v %v %v\n", this.Data.Name, this.Data.Mobile, this.Data.Status, this.Data.SendNo)
	}
	return fmt.Sprintf("\n[再接再厉] %v %v %v %v\n", this.Data.Name, this.Data.Mobile, this.Data.Address, this.Data.Status)
}

func ParseConfig(cfg string) error {
	if cfg == "" {
		log.Printf("use -c to specify configuration file")
		return errors.New("config is null")
	}
	if !file.IsExist(cfg) {
		log.Printf("config file:%v is not existent", cfg)
		return errors.New("config file is not existent")
	}
	configContent, err := file.ToTrimString(cfg)
	if err != nil {
		log.Printf("read config file:%v fail:%v", cfg, err)
		return err
	}
	var c GlobalConfig
	err = json.Unmarshal([]byte(configContent), &c)
	if err != nil {
		log.Printf("parse config file:%v fail:%v", cfg, err)
		return err
	}
	config = &c
	log.Printf("read config file :%v successfully", cfg)
	return nil
}

func HttpGet(url string) (body []byte, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("[Error] new request error:%v", err)
		return
	}
	respData, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[Error] DefaultClient do error:%v\n", err)
		return nil, err
	}
	defer respData.Body.Close()
	bodyByte, err := ioutil.ReadAll(respData.Body)
	if err != nil {
		log.Printf("[Error] ReadAll do error:%v\n", err)
		return nil, err
	}
	return bodyByte, nil
}

func QueryISZCloud() (interface{}, error) {
	var result []ISZCloudResp
	var path string = "/service/apply-win-query/%v/%v?cityNo=sz"
	for idx, _ := range config.UserInfo {
		queryPath := fmt.Sprintf(path, config.UserInfo[idx].Mobile, config.UserInfo[idx].Code)
		body, err := HttpGet(config.Server + queryPath)
		if err != nil {
			log.Printf("[Error] http get error:%v", err)
			continue
		}
		var resp ISZCloudResp
		err = json.Unmarshal(body, &resp)
		if err != nil {
			log.Printf("[Error] json unmarshal error:%v", err)
			continue
		}
		log.Printf("[Info] ISZCloud :%v", resp.String())
		result = append(result, resp)
	}

	return result, nil
}

type EmailSender interface {
	SendEmail(email_addrs []string, content string, Subject string, contentType string) (err error)
}

func CreateEmailSender(mailType string) EmailSender {
	switch strings.ToUpper(mailType) {
	case "SMTP":
		return new(SmtpSender)
	default:
		return new(SmtpSender)
	}
}

type SmtpSender struct {
}

func (sender *SmtpSender) SendEmail(email_addrs []string, content string, Subject string, contentType string) (err error) {

	err = SendSmtpEmail(email_addrs, content, Subject, contentType, config.Mail.SMTPFrom)
	if err != nil {
		log.Printf("[ERROR] err=%v\n", err.Error())
		return err
	}

	return nil
}

func SendSmtpEmail(email_addrs []string, content string, Subject string, contentType string, from string) (err error) {
	if len(email_addrs) == 0 {
		log.Printf("[DEBUG] not specified email address!")
		return nil
	}

	if content == "" {
		log.Printf("[DEBUG] the content is empty!")
		return nil
	}

	tos := strings.Join(email_addrs, ";")
	log.Printf("[INFO] /sender/mail: contentType=%s, tos=%s, subject=%s, content=%s\n", contentType, tos, Subject, content)

	err = SmtpSendMail(config.Mail.SMTPSmarthost, config.Mail.SMTPAuthUsername, config.Mail.SMTPAuthPassword, from, tos, Subject, content, contentType)
	return err
}

type unencryptedAuth struct {
	smtp.Auth
}

func (a unencryptedAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	s := *server
	s.TLS = true
	return a.Auth.Start(&s)
}

func SmtpSendMail(address, username, password, from, tos, subject, body string, contentType string) error {
	// check address
	if address == "" {
		return fmt.Errorf("address is necessary")
	}

	hp := strings.Split(address, ":")
	if len(hp) != 2 {
		return fmt.Errorf("address format error")
	}

	// format tos
	arr := strings.Split(tos, ";")
	count := len(arr)
	safeArr := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if arr[i] == "" {
			continue
		}
		safeArr = append(safeArr, arr[i])
	}

	if len(safeArr) == 0 {
		return fmt.Errorf("tos invalid")
	}

	tos = strings.Join(safeArr, ";")

	// format message
	header := make(map[string]string)
	header["From"] = from
	header["To"] = tos
	header["Subject"] = fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))
	header["MIME-Version"] = "1.0"

	ct := ""
	switch contentType {
	case "html":
		ct = "text/html; charset=UTF-8"
	default:
		ct = "text/plain; charset=UTF-8"
	}

	header["Content-Type"] = ct
	header["Content-Transfer-Encoding"] = "base64"

	message := ""
	for k, v := range header {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + base64.StdEncoding.EncodeToString([]byte(body))

	// send
	auth := unencryptedAuth{
		smtp.PlainAuth(
			"",
			username,
			password,
			hp[0],
		),
	}
	log.Printf("smtp.SendMail():%v %v %v %v %v", address, auth, from, tos, message)
	err := smtp.SendMail(address, auth, from, strings.Split(tos, ";"), []byte(message))
	return err
}

func main() {
	cfgFile := flag.String("c", "cfg.json", "config data json")

	flag.Parse()

	if cfgFile == nil || *cfgFile == "" {
		flag.Usage()
		return
	}

	//解析文件到全局配置
	err := ParseConfig(*cfgFile)
	if err != nil {
		log.Printf("[Error] parser config error:%v", err)
		return
	}

	//查詢ISZ
	result, err := QueryISZCloud()
	if err != nil {
		log.Printf("[Error] query iszcloud error:%v", err)
		return
	}

	//bs, err := json.Marshal(&result)
	//if err != nil {
	//	log.Printf("[Error] json marshal")
	//	return
	//}

	//发送邮件
	emailSender := CreateEmailSender("smtp")
	emailSender.SendEmail(config.Mail.SmtpTo, fmt.Sprintf("%v", result), fmt.Sprintf("[ISZCloud][%v]口罩预约结果", time.Now().Format("2006-01-02")), "text")

	log.Printf("[Info] query iszcloud success!!!")
}
