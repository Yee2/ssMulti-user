package webui

import (
	"net/http"
	"html/template"
	"path/filepath"
	"io"
	"errors"
	"strconv"
	"github.com/Yee2/Planet-Cup/ylog"
	"encoding/json"
	"github.com/Yee2/Planet-Cup/manager"
	"code.cloudfoundry.org/bytefmt"
	"github.com/julienschmidt/httprouter"
)
var (
	view_entry *template.Template
	views map[string]*template.Template
	err error
)

var mux = &http.ServeMux{}
var logger = ylog.NewLogger("web-UI")
var tables =  manager.NewTable()
var base = template.New("base").Funcs(template.FuncMap{"ByteSize": ByteSize})

func init(){
	views = make(map[string]*template.Template)
	view_entry = template.Must(base.ParseFiles("assets/template/entry.html"))
	view_entry,err = view_entry.ParseGlob("assets/template/components/*.html")
	letItDie(err)
	files,err := filepath.Glob("assets/template/content/*.html")
	letItDie(err)
	for _,f := range files{
		views[filepath.Base(f)] = template.Must(template.Must(view_entry.Clone()).ParseFiles(f))
	}
}

func Listen(port int){
	logger.Info("启动ShadowSock服务")
	tables.Boot()

	router := httprouter.New()
	router.GET("/", index)
	router.GET("/system", system)
	router.ServeFiles("/public/*filepath", http.Dir("assets/public"))
	router.GET("/shadowsocks/:port/speed", speed)

	router.POST("/shadowsocks", add)

	router.PUT("/shadowsocks/:port", update)
	router.DELETE("/shadowsocks/:port", del)

	err := http.ListenAndServe(":8080", router)
	if err != nil{
		logger.Info("初始化Web服务器失败:%s",err)
	}
}


func letItDie(err error){
	if err != nil{
		panic(err)
	}
}
func view(w io.Writer,name string,data interface{})  {
	if tpl,ok := views[name + ".html"]; ok{
		tpl.ExecuteTemplate(w,"entry",data)
		return
	}
	panic(errors.New("视图不存在"))
}

func view_refresh(w io.Writer,name string,data interface{})  {

	view_entry = template.Must(base.ParseFiles("assets/template/entry.html"))
	view_entry,err = view_entry.ParseGlob("assets/template/components/*.html")
	letItDie(err)
	//view_entry = view_entry.Funcs(template.FuncMap{"ByteSize": ByteSize})
	files,err := filepath.Glob("assets/template/content/*.html")
	letItDie(err)
	for _,f := range files{
		views[filepath.Base(f)] = template.Must(template.Must(view_entry.Clone()).ParseFiles(f))
	}

	if tpl,ok := views[name + ".html"]; ok{
		tpl.ExecuteTemplate(w,"entry",data)
		return
	}
	panic(errors.New("视图不存在"))
}

func res_error(w io.Writer,text string){
	response := json.NewEncoder(w)
	response.Encode(struct {
		Error string `json:"error"`
		Code int `json:"code"`
	}{text,-1})
}
func res_message(w io.Writer,text string){
	response := json.NewEncoder(w)
	response.Encode(struct {
		Error string `json:"message"`
		Code int `json:"code"`
	}{text,0})
}
func ByteSize(args ...interface{}) string{
	if len(args) == 0{
		return  ""
	}
	if size,ok := args[0].(int); ok{
		return bytefmt.ByteSize(uint64(size))
	}

	if str,ok := args[0].(string); ok{
		if size,err := strconv.Atoi(str);err == nil{
			return bytefmt.ByteSize(uint64(size))
		}
	}
	return ""
}