package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/nspcc-dev/neo-go/pkg/smartcontract/nef"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/tidwall/gjson"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/yaml.v3"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)


const RPCNODEMAIN = "https://neofura.ngd.network:1927"
//const RPCNODEMAIN = "https://testneofura.ngd.network:444"
const RPCNODETEST = "https://testneofura.ngd.network:444"

type Config struct {
	Database_main struct {
		Host     string `yaml:"host"`
		Port     string `yaml:"port"`
		User     string `yaml:"user"`
		Pass     string `yaml:"pass"`
		Database string `yaml:"database"`
		DBName   string `yaml:"dbname"`
	} `yaml:"database_main"`
	Database_test struct {
		Host     string `yaml:"host"`
		Port     string `yaml:"port"`
		User     string `yaml:"user"`
		Pass     string `yaml:"pass"`
		Database string `yaml:"database"`
		DBName   string `yaml:"dbname"`
	} `yaml:"database_test"`
}

type jsonResult struct {
	Code int
	Msg string
}

type insertVerifiedContract struct {
	Hash string
	Id string
	Updatecounter string
}

type insertContractSourceCode struct {
	Hash string
	Updatecounter string
	FileName string
	Code string
}

func multipleFile(w http.ResponseWriter, r *http.Request) {

	var m1 = make(map[string]string)
	reader, err := r.MultipartReader()
	pathFile:=createDateDir("./")
	if err != nil {
		fmt.Println("stop here")
		http.Error(w,err.Error(),http.StatusInternalServerError)
		return
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		//fmt.Printf("FileName =[%S], FormName=[%s]\n", part.FileName(), part.FormName())

		if part.FileName()== "" {
			data, _ := ioutil.ReadAll(part)
			//fmt.Printf("FormName=[%s] FormData=[%s]\n",part.FormName(), string(data))
			//fmt.Println(part.FormName())
			if part.FormName() ==  "Contract" {
				m1[part.FormName()] = string(data)
				//fmt.Println(m1)
			} else if part.FormName() == "Version" {
				m1[part.FormName()] = string(data)
				//fmt.Println(m1)
			} else {
				//fmt.Println("map storage error")
			}
		} else {
			//dst,_ :=os.Create("./"+part.FileName()
			dst,_:= os.OpenFile(pathFile+"/"+part.FileName(),os.O_WRONLY|os.O_CREATE,0666)
			defer dst.Close()
			io.Copy(dst,part)
			fileExt := path.Ext(pathFile+"/"+part.FileName())
			if fileExt == ".csproj" {
				point := strings.Index(part.FileName(),".")
				tmp := part.FileName()[0:point]
				m1["Filename"] = tmp
			}


		}

	}


	chainNef:=execCommand(pathFile+"/",w,m1)
	if chainNef == "0"||chainNef=="1"||chainNef =="2"{
		return

	}
	version,sourceNef:= getContractState(w,m1)
	if sourceNef == "3"||sourceNef=="4"{
		return
	}

	version = version[20:25]
	//fmt.Println(version)
	//fmt.Println(chainNef)
	//fmt.Println(sourceNef)
	//fmt.Println(sourceNef==chainNef)

	if sourceNef==chainNef {
		cfg, err := OpenConfigFile()
		if err != nil {
			log.Fatal(" open file error")
		}
		ctx := context.TODO()
		co,_:=intializeMongoOnlineClient(cfg, ctx)

		filter:= bson.M{"hash":getContract(m1),"updatecounter":getUpdateCounter(m1)}
		result:=co.Database("test").Collection("VerifyContract").FindOne(ctx,filter)
		//fmt.Println(result.Err())
		if result.Err() != nil {
			verified:= insertVerifiedContract{getContract(m1),getId(m1),getUpdateCounter(m1)}
			insertOne, err := co.Database("test").Collection("VerifyContract").InsertOne(ctx,verified)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("Inserted a verified Contract",insertOne.InsertedID)

			rd, err:= ioutil.ReadDir(pathFile+"/")
			for _, fi := range rd {
				if fi.IsDir(){
					continue
				} else {
					fmt.Println(fi.Name())
					file,err:= os.Open(pathFile+"/"+fi.Name())
					if err != nil {
						log.Fatal(err)
					}
					defer file.Close()
					fileinfo, err := file.Stat()
					if err != nil {
						log.Fatal(err)
					}
					filesize := fileinfo.Size()
					buffer := make([]byte,filesize)
					_, err = file.Read(buffer)
					if err != nil {
						log.Fatal(err)

					}


					sourceCode := insertContractSourceCode{getContract(m1),getUpdateCounter(m1),fi.Name(),string(buffer)}
					insertOneSourceCode, err := co.Database("test").Collection("ContractSouceCode").InsertOne(ctx, sourceCode)
					if err != nil {
						log.Fatal(err)
					}
					fmt.Println("Inserted a contract source code",insertOneSourceCode.InsertedID)

					//fmt.Println(" registed buffer",buffer)
					//fmt.Println("bytes read :",bytesread)
					//fmt.Println("bytestream to string", string(buffer))
				}
			}
			fmt.Println("=================Insert verified contract in database===============")
			msg, _ :=json.Marshal(jsonResult{5,"Verify done and record verified contract in database!"})
			w.Header().Set("Content-Type","application/json")
			w.Write(msg)



		} else {
			fmt.Println("=================This contract has already been verified===============")
			msg, _ :=json.Marshal(jsonResult{6,"This contract has already been verified"})
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type","application/json")
			w.Write(msg)


		}







		//filter:= bson.D{{"hash","0xcd10d9f697230b04d9ebb8594a1ffe18fa95d9ad"}}
		//var result insertContractSourceCode
		//err = co.Database("test").Collection("ContractSouceCode").FindOne(ctx,filter).Decode(&result)
		//if err != nil {
		//	log.Fatal(err)
		//}
		//fmt.Println("Result is ===========",string(result.Buffer))



	} else {
		if version != getVersion(m1) {
			fmt.Println("=================Please change your compiler version and try again===============")
			msg, _ :=json.Marshal(jsonResult{7,"Compiler version error, Compiler verison shoud be "+version})
			w.Header().Set("Content-Type","application/json")
			w.Write(msg)
		} else {
			fmt.Println("=================Your source code doesn't match the contract on bloackchain===============")
			msg, _ :=json.Marshal(jsonResult{8,"Contract Source Code Verification error!"})
			w.Header().Set("Content-Type","application/json")
			w.Write(msg)
		}


	}

}

func createDateDir(basepath string) string  {
	folderName := time.Now().Format("20060102150405")
	fmt.Printf("%s", folderName)
	folderPath := filepath.Join(basepath, folderName)
	if _,err := os.Stat(folderPath);os.IsNotExist(err){
		os.Mkdir(folderPath,0777)
		os.Chmod(folderPath,0777)
	}
	return folderPath

}

func execCommand(dir string,w http.ResponseWriter,m map[string] string) string{
	//cmd := exec.Command("ls")
	cmd:=exec.Command("echo")
	if getVersion(m)=="3.0.0"{
		cmd= exec.Command("/Users/qinzilie/flamingo-contract-swap/Swap/flamingo-contract-swap/c/nccs")
		fmt.Println("use 3.0.0 compiler")
	} else if getVersion(m)=="3.0.2"{
		cmd = exec.Command("/Users/qinzilie/flamingo-contract-swap/Swap/flamingo-contract-swap/b/nccs")
		fmt.Println("use 3.0.2 compiler")
	} else if getVersion(m)=="3.0.3" {
		cmd = exec.Command("/Users/qinzilie/flamingo-contract-swap/Swap/flamingo-contract-swap/a/nccs")
		fmt.Println("use 3.0.3 compiler")
	} else {
		fmt.Println("===============Compiler version doesn't exist==============")
		msg, _ :=json.Marshal(jsonResult{0,"Compiler version doesn't exist, please choose 3.0.0/3.0.2/3.0.3 version"})
		w.Header().Set("Content-Type","application/json")
		w.Write(msg)
		return "0"
	}

	cmd.Dir = dir
	stdout,err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	defer stdout.Close()

	err = cmd.Start()
	if err != nil {
		fmt.Println("=============== Cmd execution failed==============")
		msg, _ :=json.Marshal(jsonResult{1,"Cmd execution failed "})
		w.Header().Set("Content-Type","application/json")
		w.Write(msg)
		return "1"

	}

	opBytes, err := ioutil.ReadAll(stdout)
	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Println(string(opBytes))
	}
	_, err = os.Lstat(dir + "bin/sc/" + m["Filename"] + ".nef")
	if !os.IsNotExist(err) {
		f, err := ioutil.ReadFile(dir+"bin/sc/"+m["Filename"]+".nef")
		if err != nil {
			log.Fatal(err)
		}
		res,err :=nef.FileFromBytes(f)
		if err != nil {
			log.Fatal("error")
		}
		//fmt.Println(res.Script)
		var result = base64.StdEncoding.EncodeToString(res.Script)


		fmt.Println("===========Now is soucre code============")
		fmt.Println(result)
		return result

	} else {
		fmt.Println("============.nef file doesn't exist===========", err)
		msg, _ :=json.Marshal(jsonResult{2,".nef file doesm't exist "})
		w.Header().Set("Content-Type","application/json")
		w.Write(msg)
		return "2"

	}
	//	fmt.Println(res.Magic)
	//	fmt.Println(res.Compiler)
	//	fmt.Println(res.Header)
	//	fmt.Println(res.Tokens)
	//	fmt.Println(res.Script)
}

func getContractState(w http.ResponseWriter,m map[string] string) (string,string) {
	rt := os.ExpandEnv("${RUNTIME}")
	fmt.Println(rt)
	var resp *http.Response
	payload, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method": "getcontractstate",
		"params":  []interface{}{
			getContract(m),
		},
		"id": 1,
	})
	if rt !="mainnet" || rt!="testnet"{
		rt = "mainnet"
	}
	switch rt {
	case "mainnet":
		resp, err = http.Post(RPCNODEMAIN, "application/json", bytes.NewReader(payload))
	case "testnet":
		resp, err = http.Post(RPCNODETEST, "application/json", bytes.NewReader(payload))
	}

	if err != nil {
		fmt.Println("=================RPC Node doesn't exsite===============")
		msg, _ :=json.Marshal(jsonResult{3,"RPC Node doesn't exsite! "})
		w.Header().Set("Content-Type","application/json")
		w.Write(msg)
		return "","3"
	}
	defer resp.Body.Close()
	//fmt.Println("response Status:", resp.Status)
	//
	//fmt.Println("response Headers:", resp.Header)

	body, _ := ioutil.ReadAll(resp.Body)

	fmt.Println("response Body:", string(body))
	if gjson.Get(string(body),"error").Exists() {
		message:=gjson.Get(string(body),"error.message").String()
		fmt.Println("================="+message+"===============")
		msg, _ :=json.Marshal(jsonResult{4,message})
		w.Header().Set("Content-Type","application/json")
		w.Write(msg)
		return "","4"
	}

	nef := gjson.Get(string(body),"result.nef.script")
	version:=gjson.Get(string(body),"result.nef.compiler").String()
	updateCounter := gjson.Get(string(body),"result.updatecounter").String()
	id := gjson.Get(string(body),"result.id").String()
	m["id"] = id
	m["updateCounter"] = updateCounter
	//fmt.Println(base64.StdEncoding.DecodeString(sourceNef))
	fmt.Println("===============Now is ChainNode nef===============")
	fmt.Println(nef.String())
	return version,nef.String()

}

func OpenConfigFile() (Config, error) {
	absPath, _ := filepath.Abs("config.yml")
	f, err := os.Open(absPath)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var cfg Config
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		return Config{}, err
	}
	return cfg, err
}

func intializeMongoOnlineClient(cfg Config, ctx context.Context) (*mongo.Client, string) {
	rt := os.ExpandEnv("${RUNTIME}")
	var clientOptions *options.ClientOptions
	var dbOnline string
	if rt != "mainnet" || rt !="testnet"{
		rt = "mainnet"
	}
	switch rt {
	case "mainnet":
		clientOptions = options.Client().ApplyURI("mongodb://" + cfg.Database_main.User + ":" + cfg.Database_main.Pass + "@" + cfg.Database_main.Host + ":" + cfg.Database_main.Port + "/" + cfg.Database_main.Database)
		dbOnline = cfg.Database_main.Database
	case "testnet":
		clientOptions = options.Client().ApplyURI("mongodb://" + cfg.Database_test.User + ":" + cfg.Database_test.Pass + "@" + cfg.Database_test.Host + ":" + cfg.Database_test.Port + "/" + cfg.Database_test.Database)
		dbOnline = cfg.Database_test.Database
	}


	clientOptions.SetMaxPoolSize(50)
	co, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal("momgo connect error")
	}
	err = co.Ping(ctx, nil)
	if err != nil {
		log.Fatal("ping mongo error")
	}
	fmt.Println("Connect mongodb success")
	return co, dbOnline
}

func getContract(m map[string] string) string {
	return m["Contract"]
}

func getVersion(m map[string] string)  string{
	return m["Version"]
}

func getUpdateCounter(m map[string] string)  string{
	return m["updateCounter"]
}

func getId(m map[string] string)  string{
	return m["id"]
}

func main() {
	fmt.Println("Server start")
	mux := http.NewServeMux()
	mux.HandleFunc("/upload",func(writer http.ResponseWriter, request *http.Request){
		multipleFile(writer,request)
	})
	mux.Handle("/metrics", promhttp.Handler())
	handler := cors.Default().Handler(mux)
	err := http.ListenAndServe("127.0.0.1:8080", handler)
	if err != nil {
		fmt.Println("listen and server error")
	}
}