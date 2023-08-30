package api

import (
	"context"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"
	"xyhelper-arkose-v2/config"
	"xyhelper-arkose-v2/har"
	"xyhelper-arkose-v2/helper"

	"gitee.com/baixudong/gospider/ja3"
	"gitee.com/baixudong/gospider/requests"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gfile"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/gogf/gf/v2/text/gstr"
)

var (
	Ja3Spec ja3.ClientHelloSpec
	reqCli  *requests.Client
)

func init() {
	// ctx := gctx.GetInitCtx()
	// 生成指纹
	Ja3Spec, err := ja3.CreateSpecWithId(ja3.HelloFirefox_Auto) //根据id 生成指纹
	if err != nil {
		log.Panic(err)
	}
	reqCli, err = requests.NewClient(context.TODO(), requests.ClientOption{
		Ja3Spec: Ja3Spec,
		H2Ja3:   true,
		Proxy:   config.PROXY,
	})
	if err != nil {
		log.Panic(err)
	}
	// ReqCli = reqCli
}

// GetToken 获取token
func GetToken(r *ghttp.Request) {
	ctx := r.Context()
	if config.AUTHKEY != "" {
		authkey := r.Get("authkey").String()
		if authkey != config.AUTHKEY {
			r.Response.WriteJsonExit(g.Map{
				"code": 0,
				"msg":  "Fail: authkey error!",
			})
			return
		}
	}

	// 生成带超时的context
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer func() {
		// g.Log().Info(ctx, "timeout", "cancel")
		cancel()
	}()
	// 如果缓存中存在 fail 标识,那么就不再请求
	if ok, err := config.Cache.Contains(ctx, "fail"); err == nil && ok {
		expire := config.Cache.MustGetExpire(ctx, "fail")
		// g.Log().Error(ctx, getRealIP(r), "cooldown time", expire.Seconds())
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  "Fail: cooldown time!",
			"wait": expire.Seconds(),
		})
		return
	}
	harRequest := &har.Request{}
	config.Cache.MustGet(ctx, "request").Scan(harRequest)
	url := harRequest.URL
	if url == "" {
		g.Log().Error(ctx, getRealIP(r), "Pleade upload har file")

		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  "Pleade upload har file",
		})
		return
	}

	headers := harRequest.Headers
	Headers := g.Map{}
	cliCookies, err := reqCli.Cookies("https://tcr9i.chat.openai.com")
	if err != nil {
		g.Log().Error(ctx, getRealIP(r), err.Error())
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  err.Error(),
		})
		return
	}
	g.Dump(cliCookies)
	for _, v := range headers {
		// 如果v.Name 以 : 开头，那么就是一个特殊的请求头，需要特殊处理
		if gstr.HasPrefix(v.Name, ":") {
			continue
		}
		if gstr.Equal(v.Name, "Cookie") {
			if len(cliCookies) > 0 {
				Headers["Cookie"] = v.Value + "; " + cliCookies[0].Name + "=" + cliCookies[0].Value
				continue
			}
			// 如果是cookie,那么就需要特殊处理, 追加一个 名称和值都随机的 cookie
			Headers["Cookie"] = v.Value + "; " + helper.GenerateRandomString() + "=" + helper.GenerateRandomString()
			continue
		}
		// "Accept-Language" 强制为 en-US,en;q=0.5
		if gstr.Equal(v.Name, "Accept-Language") {
			Headers["Accept-Language"] = "en-US,en;q=0.5"
			continue
		}
		Headers[v.Name] = v.Value
	}
	g.Dump(Headers)
	payload := harRequest.PostData.Text
	// 以&分割转换为数组
	payloadArray := gstr.Split(payload, "&")
	// 移除最后一个元素
	payloadArray = payloadArray[:len(payloadArray)-1]
	// 将 rnd=0.3046791926621015 添加到数组最后
	payloadArray = append(payloadArray, "rnd="+strconv.FormatFloat(rand.Float64(), 'f', -1, 64))
	// 以&连接数组
	payload = strings.Join(payloadArray, "&")

	defer func() {
		reqCli.CloseIdleConnections()
	}()
	response, err := reqCli.Request(ctx, "post", url, requests.RequestOption{
		Headers: Headers,
		Data:    payload,
		Cookies: harRequest.Cookies,
	})
	if err != nil {
		g.Log().Error(ctx, getRealIP(r), err.Error())
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  err.Error(),
		})
		return
	}
	defer response.Close()
	text := response.Text()
	gjson.New(text).Dump()
	token := gjson.New(text).Get("token").String()
	if token == "" {
		g.Log().Error(ctx, getRealIP(r), text)
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  "Fail: " + text,
		})
		return
	}
	// 如果不包含 sup=1|rid= 的字符串,那么就是失败了
	if !gstr.Contains(token, "sup=1|rid=") {
		// 在缓存中设置标识 fail 为 true 表示失败,时间为 5分钟,5分钟内不再请求
		wait := g.Cfg().MustGetWithEnv(ctx, "WAIT", "300").String()
		waitDuration, err := time.ParseDuration(wait + "s")
		if err != nil {
			g.Log().Error(ctx, getRealIP(r), err.Error())
			// 服务暂时不可用
			r.Response.Status = 503
			r.Response.WriteJsonExit(g.Map{
				"code": 0,
				"msg":  err.Error(),
			})
			return
		}
		err = config.Cache.Set(ctx, "fail", true, waitDuration)
		if err != nil {
			g.Log().Error(ctx, getRealIP(r), err.Error())
			// 服务暂时不可用
			r.Response.Status = 503
			r.Response.WriteJsonExit(g.Map{
				"code": 0,
				"msg":  err.Error(),
			})
			return
		}
		g.Log().Error(ctx, getRealIP(r), token)
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  "Fail: " + token,
		})
		return
	}
	g.Log().Info(ctx, getRealIP(r), token)

	r.Response.Status = response.StatusCode()
	r.Response.WriteJsonExit(g.Map{
		"code":    1,
		"msg":     "success",
		"token":   token,
		"created": gtime.Now().Unix(),
	})

}

// Upload 上传har文件
func Upload(r *ghttp.Request) {
	ctx := r.Context()

	if r.Method == "GET" {
		r.Response.WriteTpl("upload.html")
		return
	}
	// 上传文件
	files := r.GetUploadFiles("harFile")
	if len(files) == 0 {
		r.Response.WriteTpl("error.html", g.Map{
			"error": "请上传har文件",
		})
		return
	}
	names, err := files.Save("./temp")
	if err != nil {
		r.Response.WriteExit(err)
	}
	err = gfile.Move("./temp/"+names[0], "./temp/request.har")
	if err != nil {
		r.Response.WriteTpl("error.html", g.Map{
			"error": err.Error(),
		})
		return
	}
	harRequset, err := har.Parse(ctx, "./temp/request.har")
	if err != nil {
		r.Response.WriteTpl("error.html", g.Map{
			"error": err.Error(),
		})
		return
	}
	err = config.Cache.Set(ctx, "request", harRequset, 0)
	if err != nil {
		r.Response.WriteTpl("error.html", g.Map{
			"error": err.Error(),
		})
		return
	}
	// 移除 cache中的 fail 标识
	config.Cache.Remove(ctx, "fail")
	r.Response.WriteTpl("success.html")
}

// getRealIP 获取真实IP
func getRealIP(req *ghttp.Request) string {
	// 优先获取Cf-Connecting-Ip
	if ip := req.Header.Get("Cf-Connecting-Ip"); ip != "" {
		return ip
	}

	// 优先获取X-Real-IP
	if ip := req.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	// 其次获取X-Forwarded-For
	if ip := req.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	// 最后获取RemoteAddr
	ip := req.RemoteAddr
	// 处理端口
	if index := strings.Index(ip, ":"); index != -1 {
		ip = ip[0:index]
	}
	if ip == "[" {
		ip = req.GetClientIp()
	}
	return ip
}

// Ping ping
func Ping(r *ghttp.Request) {
	ctx := r.Context()
	ok, err := config.Cache.Contains(ctx, "fail")
	if err != nil {
		r.Response.Status = 503
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  err.Error(),
		})
		return
	}
	if ok {
		r.Response.Status = 503
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  "Cooldown time!",
		})
		return
	}
	harRequest := &har.Request{}
	config.Cache.MustGet(ctx, "request").Scan(harRequest)
	url := harRequest.URL
	if url == "" {
		r.Response.Status = 503
		r.Response.WriteJsonExit(g.Map{
			"code": 0,
			"msg":  "Pleade upload har file",
		})
		return
	}
	r.Response.WriteJsonExit(g.Map{
		"code": 1,
		"msg":  "success",
	})
}
