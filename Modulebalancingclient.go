/*
*

	@author: kiki
	@since: 2025/5/26
	@desc: //TODO Module Balancing client

*
*/
package main

import (
	"ModuleBalancingClient/api"
	"ModuleBalancingClient/env"
	rpc "ModuleBalancingClient/grpc"
	"ModuleBalancingClient/logmanager"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rjeczalik/notify"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

var (
	err                   error
	serverip              string
	clientmd5             string
	programwork           *env.Work
	conn                  *grpc.ClientConn
	serverconfiguration   *env.Configuration
	waitupgrade           = make(chan struct{}, 1)
	moduledownloadprocess = make(chan string, 10)
	logmar                = logmanager.InitManager()
	updatestoreprocess    = make(chan *rpc.StorerecordRequest, 10)
)

func init() {
	Programinformation()
	if serverip = env.GetIP(); strings.EqualFold(serverip, "") {
		fmt.Println("Failed to read local ip address")
		os.Exit(1)
	}
	serverip = strings.Split(serverip, ":")[0]

	fmt.Println("Server address: ", serverip)
	f, err := os.ReadFile("conf/config.yaml")
	if err != nil {
		log.Fatalf("could not open configuration file: %v", err)
		return
	}

	fbyte, err := os.ReadFile(filepath.Join(readrunpath(), "Modulebalancingclient.exe"))
	if err != nil {
		panic(err)
	}

	clientmd5 = fmt.Sprintf("%x", md5.Sum(fbyte))

	serverconfiguration = new(env.Configuration)
	if err = yaml.Unmarshal(f, serverconfiguration); err != nil {
		log.Fatalf("could not parse configuration file: %v", err)
		return
	}

	// 客户端升级的关键代码, 用于判断当前是否有重要任务在执行中
	programwork = new(env.Work)
	programwork.Workchannel = make(chan struct{}, 10)

	// 初始化日志
	logmar.RegisterBusiness(logmanager.LoggerConfig{
		BusinessName: "Expiration",
		LogDir:       fmt.Sprintf(strings.Join([]string{readrunpath(), "logs", "expiration"}, `\`)),
		MaxSize:      1,
		MaxBackups:   90,
		MinLevel:     logmanager.INFO,
	})

	// 初始化日志
	logmar.RegisterBusiness(logmanager.LoggerConfig{
		BusinessName: "Download",
		LogDir:       fmt.Sprintf(strings.Join([]string{readrunpath(), "logs", "download"}, `\`)),
		MaxSize:      1,
		MaxBackups:   90,
		MinLevel:     logmanager.INFO,
	})

	// 初始化日志
	logmar.RegisterBusiness(logmanager.LoggerConfig{
		BusinessName: "Monitor",
		LogDir:       fmt.Sprintf(strings.Join([]string{readrunpath(), "logs", "monitor"}, `\`)),
		MaxSize:      1,
		MaxBackups:   90,
		MinLevel:     logmanager.INFO,
	})

	// 初始化日志
	logmar.RegisterBusiness(logmanager.LoggerConfig{
		BusinessName: "Store",
		LogDir:       fmt.Sprintf(strings.Join([]string{readrunpath(), "logs", "store"}, `\`)),
		MaxSize:      1,
		MaxBackups:   90,
		MinLevel:     logmanager.INFO,
	})
}

func main() {
	conn, err = grpc.NewClient(fmt.Sprintf("%s:%s", serverconfiguration.GRPCServices.Host, serverconfiguration.GRPCServices.Port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Did not connect: %v", err)
	}

	defer conn.Close()

	go Expiration()
	go Updatestore(updatestoreprocess)
	go Monitor(serverconfiguration.Setting.Chkdir, moduledownloadprocess)
	go Upadte()

	for {
		select {
		case fp := <-moduledownloadprocess:
			programwork.Run()
			var modulenames = make([]string, 0)
			if _, exist := os.Stat(fp); !os.IsNotExist(exist) {
				if err = os.Remove(fp); err != nil {
					logmar.GetLogger(`Download`).Error("Failed to remove download Tag: ", fp)
				}
			}

			fp = strings.Join([]string{serverconfiguration.Setting.AODdir, strings.NewReplacer(".ddd", ".dat", ".DDD", ".dat").Replace(filepath.Base(fp))}, "/")
			logmar.GetLogger("Download").Info(fmt.Sprintf("AOD(%s) synchronization module request....", fp))
			fmt.Printf("AOD(%s) synchronization module request....\r\n", fp)

			ctxforanalyzing, celforanalyzing := context.WithCancel(context.Background())
			if modulenames, err = api.Analyzing(ctxforanalyzing, conn, fp, serverconfiguration.Setting.Common); err != nil {
				programwork.Done()
				celforanalyzing()
				logmar.GetLogger("Download").Error(fmt.Sprintf("Failed to analyzing file: %s", err.Error()))
				fmt.Printf(fmt.Sprintf("Failed to analyzing file: %s", err.Error()))
				continue
			}

			celforanalyzing()
			updatestoreprocess <- &rpc.StorerecordRequest{
				Heartbeat:     "",
				Serveraddress: serverip,
				Partnumber:    filepath.Base(fp),
				Modulenames:   modulenames,
			}

			// 判断.OK文件是否存在, 如果存在再去检查文件是否缺失
			fileok := strings.Join([]string{serverconfiguration.Setting.Common, strings.NewReplacer(".dat", ".OK", ".DAT", ".OK").Replace(filepath.Base(fp))}, `\`)
			if _, exist := os.Stat(fileok); !os.IsNotExist(exist) {
				logmar.GetLogger("Download").Info(fmt.Sprintf("%s already exists, If you need to download and check again, please delete it .OK file", filepath.Base(fileok)))
				fmt.Printf("%s already exists, If you need to download and check again, please delete it .OK file\r\n", filepath.Base(fileok))

				var existall = true
				for _, val := range modulenames {
					if _, exist = os.Stat(strings.Join([]string{serverconfiguration.Setting.Common, val}, `\`)); os.IsNotExist(exist) {
						existall = false
						fmt.Printf("Error: Partnumber(%s) Module(%s) is not found\r\n", filepath.Base(fp), val)
						logmar.GetLogger("Download").Info(fmt.Sprintf("Error: Partnumber(%s) Module(%s) is not found", filepath.Base(fp), val))
					}
				}

				if existall {
					programwork.Done()
					continue
				}

				fmt.Printf("Missing module files, try downloading again\r\n")
			}

			// 函数用于下载Module文件
			downloadfunction := func(filename string) error {
				logmar.GetLogger("Download").Info(fmt.Sprintf("Module(%s) does not exist, requesting server download", filename))
				ctxfordownload, celfordownload := context.WithCancel(context.Background())
				if err = api.Download(ctxfordownload, conn, strings.Join([]string{serverconfiguration.Setting.Common, filename}, `\`), serverip); err != nil {
					logmar.GetLogger("Download").Error("Failed to download module:", err.Error())
					celfordownload()
					return err
				}
				logmar.GetLogger("Download").Info(fmt.Sprintf("Dowmload module(%s) completed", filename))
				celfordownload()
				return nil
			}

			var isok = true
			for _, item := range modulenames {
				// 不存在即Download 存在就检查文件是否与服务端一致
				if _, exist := os.Stat(strings.Join([]string{serverconfiguration.Setting.Common, item}, `\`)); os.IsNotExist(exist) {
					if err = downloadfunction(item); err != nil {
						isok = false
						break
					}
				} else {
					var (
						crc  uint64
						size int64
					)

					logmar.GetLogger("Download").Info(fmt.Sprintf("Module(%s) file exist verifying module integrity", item))
					fmt.Printf("Module(%s) file exist verifying module integrity", item)
					if crc, size, err = env.CRC64(strings.Join([]string{serverconfiguration.Setting.Common, item}, `\`), 128*1024*1024, 8); err != nil {
						isok = false
						break
					}

					ctxforcheck, celforcheck := context.WithCancel(context.Background())
					if err = api.Checkfileinfoemation(ctxforcheck, conn, &rpc.IntegrityVerificationResponse{
						Filename: item,
						Size:     strconv.FormatInt(size, 10),
						Crc64:    strconv.FormatUint(crc, 10),
					}); err != nil {
						fmt.Println("\t ----> Failed")
						logmar.GetLogger("Download").Error(err.Error())
						logmar.GetLogger("Download").Info(fmt.Sprintf("Delete file(%s) and download again", item))
						_ = os.Remove(strings.Join([]string{serverconfiguration.Setting.Common, item}, `\`))
						celforcheck()
						if err = downloadfunction(item); err != nil {
							isok = false
							break
						}
					}

					fmt.Println("\t ----> OK")
					logmar.GetLogger("Download").Info(fmt.Sprintf("Module(%s) file integrity verification completed", item))
				}
			}

			if isok {
				f, err := os.Create(fileok)
				if err != nil {
					programwork.Done()
					logmar.GetLogger("Download").Error("Failed to create ok tag: ", err.Error())
					continue
				}
				logmar.GetLogger("Download").Info(fmt.Sprintf("All modules are ready and generated %s file", filepath.Base(fileok)))
				fmt.Printf("All modules are ready and generated %s file\r\n", filepath.Base(fileok))
				f.Close()
			} else {
				logmar.GetLogger("Download").Error("failed to download modules")
			}

			programwork.Done()
		}
	}
}

func Updatestore(source chan *rpc.StorerecordRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updatestoreservice := rpc.NewStorerecordClient(conn)
	stream, err := updatestoreservice.Updatestorerecord(ctx)
	if err != nil {
		log.Fatalf("could not create stream: %v", err.Error())
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// 客户端心跳逻辑
	go func() {
		for range ticker.C {
			select {
			case <-stream.Context().Done():
				log.Printf("Service disconnected")
				return
			default:
				select {
				case source <- &rpc.StorerecordRequest{Heartbeat: "Heartbeat"}:

				default:
					log.Println("Service channel is full, dropping message")
				}
			}
		}
	}()

	for {
		select {
		case req := <-source:
			if err = stream.Send(req); err != nil {
				fmt.Println("[Updatestore service]Disconnect from the server")
				ticker.Stop()
				var connect = false
				for i := 0; i <= 12; i++ {
					fmt.Printf("\t----> [Updatestore service]Retry(%v)\r\n", i)
					time.Sleep(time.Second * 5)
					if stream, err = updatestoreservice.Updatestorerecord(ctx); err != nil {
						continue
					}
					connect = true
					break
				}
				if connect {
				chearchan:
					for {
						select {
						case _, ok := <-source:
							if !ok {
								break chearchan
							}
						default:
							break chearchan
						}
					}

					fmt.Println("[Updatestore service]Successfully established a connection with the server")
					ticker.Reset(time.Second * 15)
					continue
				}

				log.Fatalf("could not send request: %v", err.Error())
			}
		case <-stream.Context().Done():
			log.Printf("Service disconnected")
			return
		}
	}
}

func Upadte() {
	var ticker = time.NewTicker(10 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		select {
		case <-ticker.C:
			ticker.Stop()
			if err = api.ClientUpgrade(ctx, conn, serverip, clientmd5, readrunpath(), waitupgrade); err != nil {
				fmt.Println(err.Error())
			}
			ticker.Reset(10 * time.Second)

		case <-waitupgrade:
			for {
				time.Sleep(time.Second * 10)
				if !programwork.IsWorking() {
					var command = exec.Command(
						"CMD.exe",
						"/c",
						"start",
						fmt.Sprintf("%s/bin/ModuleBalancingUpdate.exe", readrunpath()),
						"-s",
						fmt.Sprintf("%s/Modulebalancingclient.exe", readrunpath()),
						"-d",
						fmt.Sprintf("%s/temp/ModuleBalancingClient_update.exe", readrunpath()),
					)

					_ = command.Run()
					os.Exit(0)
				} else {
					fmt.Println("The task is running, wait for the task to end and start upgrading...")
				}
			}

		}
	}
}

func Expiration() {
	expirtionservice := rpc.NewExpirationpushClient(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 请求服务
	stream, err := expirtionservice.Expiration(ctx, &rpc.ExpirationPushRequest{
		Serveraddress:    serverip,
		Maxretentiondays: serverconfiguration.Setting.Maxretentiondays,
	})
	if err != nil {
		log.Fatalf("Did not connect: %v", err)
		return
	}

	for {
		if resp, err := stream.Recv(); err != nil {
			switch err {
			case io.EOF:
				log.Println("Client closed the connection")
				return
			default:
				var connect = false
				fmt.Println("[Expiration service]Disconnect from the server")
				for i := 0; i <= 12; i++ {
					fmt.Printf("\t----> [Expiration service]Retry(%v)\r\n", i)
					time.Sleep(time.Second * 5)

					if stream, err = expirtionservice.Expiration(ctx, &rpc.ExpirationPushRequest{
						Serveraddress:    serverip,
						Maxretentiondays: serverconfiguration.Setting.Maxretentiondays,
					}); err != nil {
						continue
					}
					connect = true
					break
				}

				if connect {
					fmt.Println("[Expiration service]Successfully established a connection with the server")
					continue
				}

				fmt.Println("Error receiving data:", err.Error())
				return
			}
		} else {
			// 如果 Heartbeat 字段不为空则为心跳检测不需要进行处理
			if resp.Heartbeat != "" {
				continue
			}

			programwork.Run()
			var removetag = fmt.Sprintf("%s.OK", strings.NewReplacer(".dat", "", ".DAT", "").Replace(resp.Partnumber))
			logmar.GetLogger("Expiration").Info(fmt.Sprintf("Server notifies to delete expired modules\t----> Partnumner(%s) modules(%s)\r\n", resp.Partnumber, resp.Modulename))
			if _, exist := os.Stat(strings.Join([]string{serverconfiguration.Setting.Common, removetag}, "/")); !os.IsNotExist(exist) {
				_ = os.Remove(strings.Join([]string{serverconfiguration.Setting.Common, removetag}, "/"))
				logmar.GetLogger("Expiration").Info(fmt.Sprintf("Remove completed Tag\t----> %s", removetag))
			}

			for _, item := range resp.Modulename {
				if _, exist := os.Stat(strings.Join([]string{serverconfiguration.Setting.Common, item}, "/")); !os.IsNotExist(exist) {
					_ = os.Remove(strings.Join([]string{serverconfiguration.Setting.Common, item}, "/"))
					logmar.GetLogger("Expiration").Info(fmt.Sprintf("Remove expiration module\t----> %s", strings.Join([]string{serverconfiguration.Setting.Common, item}, "/")))
				}
			}

			logmar.GetLogger("Expiration").Info("Delete expiation module completed")
			programwork.Done()
		}
	}
}

func Monitor(monitorpath string, noticechan chan string) {
	monitordir, err := fsnotify.NewWatcher()
	if err != nil {
		logmar.GetLogger("Monitor").Error(fmt.Sprintf("Monitor Path NewWatcher Error: %s", err.Error()))
		return
	}

	if err = monitordir.Add(monitorpath); err != nil {
		logmar.GetLogger("Monitor").Error(fmt.Sprintf("Add Monitor Path Error: %s", err.Error()))
		return
	}

	fmt.Printf("Start listening to directory -> (%s)\r\n", monitorpath)
	logmar.GetLogger("Monitor").Info(fmt.Sprintf("Start listening to directory -> (%s)", monitorpath))

	for {
		select {
		case event, ok := <-monitordir.Events:
			if !ok {
				return
			}

			programwork.Run()
			if event.Op&fsnotify.Create == fsnotify.Create {
				logmar.GetLogger("Monitor").Info("Folder change detected: %s", event.Name)
				time.Sleep(time.Second * 1)
				if info, err := os.Stat(event.Name); err != nil {
					programwork.Done()
					logmar.GetLogger("Monitor").Error(fmt.Sprintf("Get Path Info Error: %s", err.Error()))
					continue
				} else {
					if info.IsDir() {
						programwork.Done()
						logmar.GetLogger("Monitor").Info(fmt.Sprintf("Listen to folder creation: %s", event.Name))
						continue
					}
					logmar.GetLogger("Monitor").Info(fmt.Sprintf("Monitor new create: %s", event.Name))
					noticechan <- event.Name
					programwork.Done()
				}
			}
		case err = <-monitordir.Errors:
			logmar.GetLogger("Monitor").Error("Monitor Path Error: %s", err.Error())
			return
		}
	}
}

// Monitorpath 监听客户端上传DDD文件
func Monitorpath(monitorpath string, noticechan chan string) {
	var (
		monitorchannel = make(chan notify.EventInfo, 20)
		monitorevent   = notify.Create
	)

	if err = notify.Watch(monitorpath, monitorchannel, monitorevent); err != nil {
		fmt.Println(err)
		return
	}

	defer notify.Stop(monitorchannel)
	fmt.Printf("Start listening to directory -> (%s)\r\n", monitorpath)
	logmar.GetLogger("Monitor").Info(fmt.Sprintf("Start listening to directory -> (%s)", monitorpath))

	for {
		select {
		case cre := <-monitorchannel:
			programwork.Run()
			time.Sleep(time.Second * 5)
			switch cre.Event() {
			case notify.Create:
				logmar.GetLogger("Monitor").Info(fmt.Sprintf("Monitor new create: %s", cre.Path()))
				noticechan <- cre.Path()
				programwork.Done()
			default:
				programwork.Done()
			}
		}
	}
}

// readrunpath 获取程序运行路径
func readrunpath() string {
	exePath, err := os.Executable()
	if err == nil {
		// 解析符号链接
		if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
			exePath = realPath
		}

		// 检查是否为go run临时文件
		tempDir := filepath.ToSlash(os.TempDir())
		absPath := filepath.ToSlash(exePath)
		if !strings.Contains(absPath, tempDir) ||
			(!strings.Contains(absPath, "go-build") && (!strings.Contains(absPath, "go-run"))) {
			return filepath.Dir(exePath)
		}
	}

	// 2. 尝试通过调用栈获取项目根目录
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		currentDir := filepath.Dir(filename)
		// 向上查找项目特征文件
		for depth := 0; depth < 10; depth++ {
			// 检查项目特征文件
			checks := []string{"go.mod", ".git", "main.go"}
			for _, check := range checks {
				if _, err := os.Stat(filepath.Join(currentDir, check)); err == nil {
					return currentDir
				}
			}

			// 向上一级目录
			parent := filepath.Dir(currentDir)
			if parent == currentDir {
				break
			}
			currentDir = parent
		}
	}

	// 3. 回退到当前工作目录
	if wd, err := os.Getwd(); err == nil {
		return wd
	}

	// 4. 最终回退
	return "."
}

func Programinformation() {
	var programname = `
███╗   ███╗ ██████╗ ██████╗ ██╗   ██╗██╗     ███████╗    ██████╗  █████╗ ██╗      █████╗ ███╗   ██╗ ██████╗██╗███╗   ██╗ ██████╗        ██████╗
████╗ ████║██╔═══██╗██╔══██╗██║   ██║██║     ██╔════╝    ██╔══██╗██╔══██╗██║     ██╔══██╗████╗  ██║██╔════╝██║████╗  ██║██╔════╝       ██╔════╝
██╔████╔██║██║   ██║██║  ██║██║   ██║██║     █████╗      ██████╔╝███████║██║     ███████║██╔██╗ ██║██║     ██║██╔██╗ ██║██║  ███╗█████╗██║     
██║╚██╔╝██║██║   ██║██║  ██║██║   ██║██║     ██╔══╝      ██╔══██╗██╔══██║██║     ██╔══██║██║╚██╗██║██║     ██║██║╚██╗██║██║   ██║╚════╝██║     
██║ ╚═╝ ██║╚██████╔╝██████╔╝╚██████╔╝███████╗███████╗    ██████╔╝██║  ██║███████╗██║  ██║██║ ╚████║╚██████╗██║██║ ╚████║╚██████╔╝      ╚██████╗
╚═╝     ╚═╝ ╚═════╝ ╚═════╝  ╚═════╝ ╚══════╝╚══════╝    ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝ ╚═════╝╚═╝╚═╝  ╚═══╝ ╚═════╝        ╚═════╝
`

	fmt.Printf("%s\r\n\r\n", programname)
}
