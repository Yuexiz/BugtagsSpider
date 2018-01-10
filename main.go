package main

import (
	"flag"
	"fmt"
	"github.com/bitly/go-simplejson"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"github.com/garyburd/redigo/redis"
	"encoding/json"
	"time"
	"strconv"
)

var issueId string	//建图片文件夹需要字段 后期可优化
var uid string		//uid 需求变更 已经可以删除 //这个uid可以替换成bool 如果正确就是uid操作
var addIssueType = true	//控制增量更新或者是完整更新 true增量更新 ,用第三个参数是否有值控制
var envStruct env	//环境有关参数结构体
var stopNum int
var startTimeString int

type env struct { // 当前环境名 环境id
	envName string
	envId string
}

const (
	beta = "1529581119996976"
	live = "1569703749232718"
)

// 这个方法可以从本地读取文件内的字符串,以作cookie使用
func readCookieFromFile() string {
	inputFile := "~/bugtagCookie"
	buf, err := ioutil.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "File Error: %s\n", err)
		// panic(err.Error())
	}
	fmt.Printf("%s\n", string(buf))
	return string(buf)
}

func main() {
	startTime :=time.Now().Format("2006-01-02 15:04:05")  //当前时间的字符串，2006-01-02 15:04:05据说是golang的诞生时间，固定写法
	fmt.Println("程序启动时间",startTime)
	stopNum = 0
	// 爬虫启动时间戳
	startTimeString = int(time.Now().Unix()) * 1000

	//读取命令行参数
	flag.Parse()
	var issue string
	if len(flag.Args()) == 0 {
		log.Fatalln("后面必须加参数呦.如果想下载所有图片请在运行命令后加all,下载单独issue的请在后面加上url中~/issues/后接的数字字符串,嘻嘻嘻 另外如果下载没有开始,一定是issue字符串找错了呦🤣")
	}

	//这里加上新的需求,查找uid ,目标名:user_data,下载下来是一个txt文件,若txt内内容匹配,则输出issue url
	if flag.Args()[0] == "uid" && len(flag.Args()[1]) > 0 {
		getEnvUrl(flag.Args()[1]) //这个可以往前提了,目前uid没有问题,看图片需求了
		
		uid = flag.Args()[1]

		if len(flag.Args()) == 3  {
			addIssueType = false
		}
		println("是增量更新么:", addIssueType)

		getUidListUrl()

	} else { // TODO 该优化 目前不需要这段逻辑了,暂不修改了

		if flag.Args()[0] == "all" {
			issue = "bugPic"
		} else {
			issue = "issueId:" + flag.Args()[0]
		}
		//创建下载目录
		mkdirFolder(issue)
		issueId = "./" + issue + "/"

		if flag.Args()[0] == "all" {
			getListUrl()
		} else {
			separateIssue(flag.Args()[0], 1, "", 0)
		}
	}

	writeInfoToRedis(envStruct.envName + "success", startTimeString) //环境字段(目前是”beta” 与 “live”) + “sucess”作为key
	timeEndStr:=time.Now().Format("2006-01-02 15:04:05")
	fmt.Println("程序结束时间",timeEndStr)

}

// TODO 修复了,但是还有改进空间
func getEnvUrl(arg string) { //不知道为啥要写这个方法...但是感觉写了反而麻烦了
	b, error := strconv.Atoi(arg)
	if error != nil || (b < 0 || b > 1) { // TODO 我记得括号里有种写法来着,忘记了
		panic("参数有误,重新来起")
	}
	if b == 0 {
		envStruct = env{"beta", beta}
	} else { // 其实应该是else if 但是目前还是这种设定得了 ,以后有问题再修改
		envStruct = env{"live", live}
	}
	println("当前环境:",envStruct.envName,"环境id:",envStruct.envId)

}

func getInfoFromRedis(key string) (int) {
	c, err := redis.Dial("tcp", "127.0.0.1:6379")
	if err != nil {
		fmt.Println("Connect to redis error", err)
		return 0
	}
	defer c.Close()

	timeStamp, e := redis.Int(c.Do("GET", key))
	if e != nil && timeStamp < 1 {
		return 0
	}
	return timeStamp
}

// TODO 两个writeToRedis一类的方法 可以做抽离,等着我研究研究 感觉结构体不错  可以抽离到另一个文件
func writeInfoToRedis(key string, value int) {

	c, err := redis.Dial("tcp", "127.0.0.1:6379")
	if err != nil {
		fmt.Println("Connect to redis error", err)
		return
	}
	defer c.Close()

	if value != 0 {
		_, err2 := c.Do("SET", key, value)
		if err2 != nil {
			fmt.Println("存数据失败",err2)
		}
		return
	}

	timeStamp, e := redis.Int(c.Do("GET", "temp" + key))
	if e != nil && timeStamp < 1 {
		return
	}
	_, err2 := c.Do("SET", key, timeStamp)
	//println("存issue的key",key,"时间戳",timeStamp)

	if err2 != nil {
		fmt.Println("存数据失败",err2)
	}

}

func writeTempInfoToRedis(key string, value int) { //目的为了存储一份本次该issue第一个bug出现时间,待整个issue(运行至上一次的第一个bug时间),将值取出赋值到标准key中

	c, err := redis.Dial("tcp", "127.0.0.1:6379")
	if err != nil {
		fmt.Println("Connect to redis error", err)
		return
	}
	defer c.Close()

	_, err2 := c.Do("SET", "temp" + key, value)
	//println("temp暂存issue的key","temp" + key,"时间戳",value)

	if err2 != nil {
		fmt.Println("存数据失败",err2)
	}

}

func writeToRedis(uid string, time string, addUrls string){ //这里需要添加参数.传入一个uid ,是主key value是一个map{}map的key是time ,value是一个字符串是正常点击url+"-"+json的url

	key := uid
	imapGet := make(map[string]string)

	c, err := redis.Dial("tcp", "127.0.0.1:6379")
	if err != nil {
		fmt.Println("Connect to redis error", err)
		return
	}
	defer c.Close()

	is_key_exit, err := redis.Bool(c.Do("EXISTS", key))
	if err == nil && is_key_exit { // 此处逻辑已重写,需验证 验证完毕

		valueGet, err := redis.Bytes(c.Do("GET", key))
		errShal := json.Unmarshal(valueGet, &imapGet)

		//得到imapGet字典,如果有则加key,如果没有则也是加key
		if errShal != nil {
			fmt.Println(err)
		}
	}

	if len(time) > 0 {
		imapGet[time] = addUrls

		value, _ := json.Marshal(imapGet)

		//先删除一下
		_, err = c.Do("DEL", key)
		if err != nil {
			fmt.Println("redis delelte failed:", err)
		}

		_, err := c.Do("SETNX", key, value) // TODO 这里的操作不知道能不能修改一下,这样操作慢了点
		if err != nil {
			fmt.Println("存数据失败",err)
		}

	}

}

func mkdirFolder(folderName string) {

	err := os.Mkdir(folderName, 0755)
	if err != nil {
		log.Println(err)
	}

}

func getUidListUrl() {

	listPage := getUidListPages()
	println("一共有",listPage,"页")
	for i := 1; i < listPage + 1; i++ { // "has_more": false 这个参数一直是false,想想更好的办法 解决完毕我真棒
		println("正在检索第",i,"页")
		//urls := fmt.Sprintf("https://work.bugtags.com/api/apps/" + envStruct.envId + "?page=%d", i)
		//https://work.bugtags.com/api/apps/1569703749232718/issues/?page=1
		urls := fmt.Sprintf("https://work.bugtags.com/api/apps/" + envStruct.envId + "/issues/?page=%d", i)

		respBody, isSuccess := getResBody(urls)
		if isSuccess {
			println(urls)
			getUserData(respBody)
		}

	}
}

func getUidListPages() (int) {
	listUrl := "https://work.bugtags.com/api/apps/" + envStruct.envId + "/issues/page/"
	respBody, isSuccess := getResBody(listUrl)
	if isSuccess {
		js, err := simplejson.NewJson(respBody)

		if err != nil {
			fmt.Println("getUidListPagesFailure1 : ", err)
		}

		page, _ := js.Get("data").Get("total").Int()
		return page/20 +1

	}
	panic("页数获得有问题")
}

func getUserData(respBody []byte) {
	js, err := simplejson.NewJson(respBody)
	if err != nil {
		fmt.Println("getUserDataFailure1 : ", err)
	}

	cn_body, _ := js.Get("data").Get("list").Array()

	for _, di := range cn_body {
		//就在这里对di进行类型判断

		md, _ := di.(map[string]interface{})

		//这里取tag,为了拼接可以阅读的url链接
		tagArr, _ := md["tags"].([]interface{})
		tagStr := ""
		for _, di := range tagArr {
			snap, _ := di.(map[string]interface{})
			iName, _ := snap["id"].(json.Number)
			tagStr = string(iName)

		}

		arr, _ := md["snapshots"].([]interface{})

		for _, di := range arr { // TODO 这个array为什么这么取,我也是一愣...但是好用就行
			snap, _ := di.(map[string]interface{})

			var issueName string
			iName, _ := snap["issue_id"].(string)
			issueName = iName
			//println("这个issue上次",getInfoFromRedis(envStruct.envName + iName))
			separateIssue(issueName, 1, tagStr, getInfoFromRedis(envStruct.envName + iName))
		}
	}

}

//每个bug一张
func getListUrl() {
	listPage := getUidListPages()
	for i := 1; i < listPage +1; i++ {
		//https://work.bugtags.com/api/apps/1569703749232718/issues/?page=1
		urls := fmt.Sprintf("https://work.bugtags.com/api/apps/" + envStruct.envId + "/issues/?page=%d", i)

		respBody, isSuccess := getResBody(urls)
		if isSuccess {
			readyJsonList(respBody)
		}

	}
}

func getResBody(urls string) ([]byte, bool) {

	j, _ := cookiejar.New(nil)
	client := &http.Client{Jar: j}

	req, err := http.NewRequest("GET", urls, nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("client.Do Failure : ", err)
		return nil, false
	}
	defer resp.Body.Close() //这句灰常关键.....defer大法好

	//置入cookie
	rawCookies := "cgr_user_id=210ecd7c-2d5e-4eaa-b210-a77cefbf5973; user_id=1575063648214000; access_token=QKZFHXVjFfMTU3NTA2MzY0ODIxNDAwMF9lWFZsZUdrdWVrQm5iV0ZwYkM1amIyMD1fMGI4N2ViZjA1MDY5OGM5NjdmNGMzYjUxMTM0NzI3NzBfMTUwODMyNTg0MA%3D%3D; Hm_lvt_faf40d1dd41f0d73bf8a504980865f5c=1505440733,1505701973,1505714042; Hm_lpvt_faf40d1dd41f0d73bf8a504980865f5c=1505733865; app_id=1529581119996976"

	header := http.Header{}
	header.Add("Cookie", rawCookies)
	request := http.Request{Header: header}
	clist := request.Cookies()

	urlX, _ := url.Parse("https://work.bugtags.com")
	j.SetCookies(urlX, clist)

	// Fetch Request
	resp, err = client.Do(req)
	if err != nil { // TODO 网络请求失败就中止循环,后期可以修改
		fmt.Println("网络请求Failure : ", err)

		return nil, false
	}

	// 读取body
	respBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("读取失败Failure : ", err)
		return nil, false
	}

	return respBody, true

}

//每个bug一张图版本的
func readyJsonList(respBody []byte) {

	js, err := simplejson.NewJson(respBody)
	if err != nil {
		fmt.Println("readyJsonListFailure1 : ", err)
	}

	cn_body, _ := js.Get("data").Get("list").Array()

	for _, di := range cn_body {

		//就在这里对di进行类型判断
		md, _ := di.(map[string]interface{})

		arr, _ := md["snapshots"].([]interface{})

		for _, di := range arr {
			snap, _ := di.(map[string]interface{})

			var issueName string
			iName, _ := snap["issue_id"].(string)
			issueName = iName

			separateIssue(issueName, 1, "", 0)
		}

	}

}

func separateIssue(issue_id string, page int, issueTag string, lastTimeStamp int) {
	if stopNum > 20 && addIssueType{ // 这段逻辑正常了
		writeInfoToRedis(envStruct.envName + "success", startTimeString)
		println("本次增量爬取结束")
		os.Exit(0) // TODO 这个不知道是否直接终止了所有defer的操作
	}
	issueUrl := "https://work.bugtags.com/api/apps/" + envStruct.envId + "/feeds/?tag_id=" + issue_id + "&page=%d"

	urls := fmt.Sprintf(issueUrl, page)
	println(urls)
	//拼接的请求中 "has_more": true, 如果是true就递归请求 然而实现的却不是递归 这就有点好笑了
	//如果想要理解递归....就先要理解递归
	isFirst := false

	respBody, isSuccess := getResBody(urls)
	if !isSuccess { return } // TODO 这里可以做如果失败的再次请求....到时候试试看吧
	if len(uid) > 0{ // TODO 这里uid将要改成bool形式
		if page == 1 {
			isFirst = true
		}
		readyUidIssueJsonList(respBody, issue_id, page, urls, issueTag, isFirst, lastTimeStamp)
	} else {
		readyIssueJsonList(respBody, issue_id, page)
	}
}

func readyUidIssueJsonList(respBody []byte, issue_id string, page int, issueUrl string, issueTag string, isFirst bool, lastTimeStamp int) {
	js, err := simplejson.NewJson(respBody)
	if err != nil {
		fmt.Println("readyUidIssueJsonListFailure1 : ", err)
		return
	}

	cn_body, _ := js.Get("data").Get("list").Array()
	//pool := New(100)
	var issueFullID string
	for _, di := range cn_body {
		//就在这里对di进行类型判断
		md, _ := di.(map[string]interface{})
		//fmt.Println(md)
		userDataMap, _ := md["occurrence_info"].(map[string]interface{})
		issueTime, _ := userDataMap["time_fmt"].(string)
		snapUrl, _ := userDataMap["user_data"].(string)
		//fmt.Println(snapMap)
		issueName, _ := md["issue_id"].(string)
		issueFullID = issueName


		if len(snapUrl) > 0 && len(issueName) > 0 && len(issueUrl) > 0 && len(issueTag) > 0 && len(issueTime) > 0  {
			issueTimeStampFloat, e := (userDataMap["time"].(json.Number)).Float64()
			if addIssueType && isFirst { // 环境字段+issueId

				if e == nil {
					issueTimeStamp := int(issueTimeStampFloat)
					if issueTimeStamp <= getInfoFromRedis(envStruct.envName + "success") {
						stopNum += 1
						println("现在这么多个了:", stopNum)
					}
					writeTempInfoToRedis((envStruct.envName + issueName), issueTimeStamp)

					isFirst = false

				}

			}

			if addIssueType && int(issueTimeStampFloat) < (lastTimeStamp - 7200000) {
				println("时间小于上次,中止本issue爬取", int(issueTimeStampFloat), lastTimeStamp)
				writeInfoToRedis(envStruct.envName + issueName, 0)
				return
			}
			// 暂时关掉协程的限制
			go getUidTxt(snapUrl, issueName, issueUrl, issueTag, issueTime)

		} else {
			println("有点小问题")
		}

	}

	hasNextPage, _ := js.Get("data").Get("has_more").Bool()
	// TODO 这个暂时关闭了
	//fmt.Println("这个错误有没有下一页", hasNextPage)
	if hasNextPage == true {
		separateIssue(issue_id, page+1, issueTag, lastTimeStamp)
	} else {//完成了 存储一份
		writeInfoToRedis(envStruct.envName + issueFullID, 0)
	}

}

//这个是图片的json解析
func readyIssueJsonList(respBody []byte, issue_id string, page int) {
	js, err := simplejson.NewJson(respBody)
	if err != nil {
		fmt.Println("readyIssueJsonListFailure1 : ", err)
		return
	}

	cn_body, _ := js.Get("data").Get("list").Array()
	pool := New(1000)

	for _, di := range cn_body {
		//就在这里对di进行类型判断

		md, _ := di.(map[string]interface{})

		snapMap, _ := md["snapshot"].(map[string]interface{})
		snapUrl, _ := snapMap["url"].(string)

		issueName, _ := snapMap["issue_id"].(string)
		issueTime, _ := snapMap["created_at"].(string)
		var issueNameAndTime string
		issueNameAndTime = issueName + "," + issueTime

		pool.Add(1)
		go func() {
			//time.Sleep(time.Second)
			go getPicture(snapUrl, issueNameAndTime) //这里调用 每个Bug 封面图
			pool.Done()
		}()

	}
	pool.Wait()
	hasNextPage, _ := js.Get("data").Get("has_more").Bool()
	// TODO 暂时关闭了
	//fmt.Println("这个错误有没有下一页", hasNextPage)
	if hasNextPage == true {
		separateIssue(issue_id, page+1, "", 0)
	}

}

func getUidTxt(urls string, name string, issueUrl string, issueTag string, issueTime string) {
	if len(urls) > 0 && len(name) > 0 {
		res, e := http.Get(urls)
		if e != nil || res.Body == nil{
			return
		}
		resbody, err := ioutil.ReadAll(res.Body)
		defer res.Body.Close()
		if err != nil || resbody == nil  {
			fmt.Println("FailureGetUidTxt : ", err)
			return
		}

		js, err := simplejson.NewJson(resbody)
		if err != nil {
			fmt.Println("FailureGetUidTxt : ", err)
			return
		}

		a:= js.MustMap()
		if a["uid"] == nil {
			return
		}
		nUid := a["uid"].(string)
		u := "https://work.bugtags.com/apps/" + envStruct.envId + "/issues/" + name +"/tags/" + issueTag +"?page=1"

		writeToRedis(nUid, issueTime, u + "-" + issueUrl)
		// TODO 不要了的检索uid的方法
		//println("测试下这个能不能打印出来:",nUid)
		//if nUid == uid {
		//	u := "https://work.bugtags.com/apps/1529581119996976/issues/" + name +"/tags/1581175972827397" + issueTag +"?page=1"
		//
		//	println("找到啦,url是:",u)
		//	println("json的url是",issueUrl)
		//}

	}
}

func getPicture(urls string, name string) {

	if len(urls) > 0 && len(name) > 0 {
		res, _ := http.Get(urls)

		file, _ := os.Create(issueId + name + ".jpg")
		io.Copy(file, res.Body)
		defer res.Body.Close()
		fmt.Println(name)
	}

}
