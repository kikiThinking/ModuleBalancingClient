/*
*

	@author: kiki
	@since: 2025/5/26
	@desc: //TODO

*
*/

package api

import (
	"ModuleBalancingClient/env"
	rpc "ModuleBalancingClient/grpc"
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
)

var err error

// Analyzing 上传AOD文件解析, 如果有解析失败的就本地尝试解析
func Analyzing(ctx context.Context, conn *grpc.ClientConn, fp, common string) ([]string, error) {
	f, err := os.ReadFile(fp)
	if err != nil {
		return nil, err
	}

	var analyzingservice = rpc.NewModuleClient(conn)
	result, err := analyzingservice.Analyzing(ctx, &rpc.AnalyzingRequest{Fbytes: f, Filename: filepath.Base(fp)})
	if err != nil {
		return nil, err
	}

	// 19 没有解析成功的列表
	if len(result.Analyzingerror) != 0 {
		fmt.Println("Some files were not parsed by the server, attempt to parse locally")
		for _, value := range result.Analyzingerror {
			fmt.Printf("File(%s)\r\n", value)
			if _, exist := os.Stat(strings.Join([]string{common, value}, `\`)); os.IsNotExist(exist) {
				return nil, fmt.Errorf("failed to analyzing, AOD(%s) Module(%s) is not found\r\n", filepath.Base(fp), value)
			}

			lf, err := os.ReadFile(strings.Join([]string{common, value}, `\`))
			if err != nil {
				return nil, fmt.Errorf("failed to analyzing, maeeage(%s)\r\n", err.Error())
			}

			matchstr, err := regexp.Compile(`(?i)modulename\d*.*`) //
			if err != nil {
				return nil, err
			}

			var buf = bufio.NewScanner(bytes.NewReader(lf))
			for buf.Scan() {
				if buf.Err() != nil {
					break
				}

				var modulename = matchstr.FindString(buf.Text())
				if strings.EqualFold(modulename, "") || len(strings.Split(modulename, "=")) != 2 {
					continue
				}

				fmt.Printf("+ %s\r\n", modulename)
				result.Modulename = append(result.Modulename, strings.Split(modulename, "=")[1])
			}
		}
	}

	return env.RemoveDuplicates(result.Modulename), nil
}

func Download(ctx context.Context, conn *grpc.ClientConn, fp, serverip string) error {
	var (
		stream   grpc.ServerStreamingClient[rpc.ModulePushResponse]
		dlclient = rpc.NewModuleClient(conn)
	)

	// 判断文件夹是否存在, 如果不存在创建文件夹, 防止创建文件时发生panic
	if _, exist := os.Stat(filepath.Dir(fp)); os.IsNotExist(exist) {
		if err = os.MkdirAll(fp, 0777); err != nil {
			return err
		}
	}

	if stream, err = dlclient.Push(ctx, &rpc.ModuleDownloadRequest{Serveraddress: serverip, Filename: filepath.Base(fp), Offset: 0}); err != nil {
		return err
	}

	f, err := os.Create(fp)
	if err != nil {
		return err
	}

	headers, err := stream.Header()
	if err != nil {
		return err
	}

	if len(headers) == 0 {
		return errors.New("failed to header if not found")
	}

	var (
		crc    = headers.Get("crc64")
		size   int64
		modify int64
		create int64
		fcrc   uint64
	)

	filesize, err := strconv.ParseInt(headers.Get("size")[0], 10, 64)
	if err != nil {
		return err
	}

	var bar = env.Processprintstruct{Size: int(filesize)}
	bar.Initialization()

	var offset = 0
	for {
		var response *rpc.ModulePushResponse
		if response, err = stream.Recv(); err != nil {
			if err == io.EOF {
				// 下载完成
				break
			}

			// 这里实现断点续传, 如果重新和服务端建立连接则重offset处继续下载
			fmt.Printf("(%s)Service connect close, wait downloading...\r\n", err.Error())
			var connect = false
			for i := 0; i <= 12; i++ {
				log.Printf("waiting of retry offset(%v)...\t\n", offset)
				time.Sleep(time.Second * 5)
				if stream, err = dlclient.Push(ctx, &rpc.ModuleDownloadRequest{Serveraddress: serverip, Filename: filepath.Base(fp), Offset: int64(offset)}); err != nil {
					continue
				}
				connect = true
				break
			}

			if connect {
				continue
			}

			return fmt.Errorf("failed to download file(%s), retry more then 5 times", filepath.Base(fp))
		}

		// 文件已经下载完毕
		if response.Completed {
			break
		}

		number, err := f.Write(response.Content)
		if err != nil {
			return err
		}

		offset += number
		bar.ProcessPrint(offset)
	}

	fmt.Printf("\r\nDownload (%s)\t----> finish\r\n\r\n", fp)

	_ = f.Close()

	if modify, err = strconv.ParseInt(headers.Get("modify-time")[0], 10, 64); err != nil {
		return err
	}

	if create, err = strconv.ParseInt(headers.Get("create-time")[0], 10, 64); err != nil {
		return err
	}

	if err = env.Changefiletime(fp, modify, create); err != nil {
		return nil
	}

	// 计算下载下来的文件的CRC, 比对是否与服务端提供的一致
	if fcrc, size, err = env.CRC64(fp, 128*1024*1024, 8); err != nil {
		return err
	}

	// 判断下载的文件大小与服务端提供的是否一致
	if size != filesize {
		fmt.Printf("src: %v dest: %v\n", size, filesize)
		return errors.New("files are different sizes")
	}

	if !strings.EqualFold(crc[0], strconv.FormatUint(fcrc, 10)) {
		return errors.New("the crc64 values are inconsistent, and the file may have been damaged during the download process")
	}

	return nil
}

// Checkfileinfoemation 用于判断本地文件与服务端文件 size|crc是否一致
func Checkfileinfoemation(ctx context.Context, conn *grpc.ClientConn, src *rpc.IntegrityVerificationResponse) error {
	var (
		informationclient = rpc.NewModuleClient(conn)
	)

	var response *rpc.IntegrityVerificationResponse
	if response, err = informationclient.IntegrityVerification(ctx, &rpc.IntegrityVerificationRequest{Filename: src.Filename}); err != nil {
		return err
	}

	if response.Size != src.Size || response.Crc64 != src.Crc64 {
		return fmt.Errorf("the local file is inconsistent with the server Local(%v|%v) Server(%v|%v)", src.Size, src.Crc64, response.Size, response.Crc64)
	}

	return nil
}

// ClientUpgrade 客户端程序升级函数
func ClientUpgrade(ctx context.Context, conn *grpc.ClientConn, serverip, clientmd5, apprunpath string, upgradechannel chan struct{}) error {
	var updateservice = rpc.NewClientCheckClient(conn)

	checkres, err := updateservice.MD5(ctx, &rpc.MD5Request{})
	if err != nil {
		return err
	}

	if checkres.MD5 == clientmd5 {
		return nil
	}

	fmt.Println("\r\n\r\n********** Client programs need to be updated **********")
	fmt.Println("> Server MD5: ", checkres.MD5)
	fmt.Println("> Client MD5: ", clientmd5)
	time.Sleep(time.Second * 10)

	stream, err := updateservice.Data(ctx, &rpc.DataRequest{Offset: 0, Serveraddress: serverip})
	if err != nil {
		return err
	}

	headers, err := stream.Header()
	if err != nil {
		return err
	}

	if len(headers) == 0 {
		return errors.New("failed to header if not found")
	}

	var (
		filesize  int64
		servermd5 = headers.Get("MD5")[0]
		filename  = headers.Get("Filename")[0]
	)

	f, err := os.Create(filepath.Join(apprunpath, "temp", "ModuleBalancingClient_update.exe"))

	filesize, err = strconv.ParseInt(headers.Get("Size")[0], 10, 64)
	if err != nil {
		fmt.Println(err.Error())
	}

	var hash = md5.New()

	multiWriter := io.MultiWriter(f, hash)

	var bar = env.Processprintstruct{Size: int(filesize)}
	bar.Initialization()

	var offset = 0
	for {
		var response *rpc.DataResponse
		if response, err = stream.Recv(); err != nil {
			if err == io.EOF {
				// 下载完成
				break
			}

			// 这里实现断点续传, 如果重新和服务端建立连接则重offset处继续下载
			fmt.Printf("(%s)Service connect close, wait downloading...\r\n", err.Error())
			var connect = false
			for i := 0; i <= 12; i++ {
				log.Printf("waiting of retry offset(%v)...\t\n", offset)
				time.Sleep(time.Second * 5)
				if stream, err = updateservice.Data(ctx, &rpc.DataRequest{Serveraddress: serverip, Offset: int64(offset)}); err != nil {
					continue
				}
				connect = true
				break
			}

			if connect {
				continue
			}

			return fmt.Errorf("failed to download file(%s), retry more then 5 times", filename)
		}

		number, err := multiWriter.Write(response.Content)
		if err != nil {
			panic(err)
		}

		offset += number
		bar.ProcessPrint(offset)
	}

	_ = f.Close()
	if servermd5 != fmt.Sprintf("%x", hash.Sum(nil)) {
		return errors.New("the servermd5 is inconsistent")
	}

	fmt.Println("\r\nDownload successful, waiting for upgrade...")
	upgradechannel <- struct{}{}
	return nil
}
