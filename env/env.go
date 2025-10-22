/*
*

	@author: kiki
	@since: 2025/5/28
	@desc: //TODO

*
*/

package env

import (
	"fmt"
	"hash/crc64"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/redmask-hb/GoSimplePrint/goPrint"
)

var (
	err error
)

// Work 监听程序是否在执行重要任务
type Work struct {
	Workchannel chan struct{}
}

type Processprintstruct struct {
	Size        int
	conversion  int
	processspri *goPrint.Bar
}

type Configuration struct {
	Setting struct {
		Maxretentiondays int64  `yaml:"Maxretentiondays"`
		Common           string `yaml:"Common"`
		Chkdir           string `yaml:"Chkdir"`
		AODdir           string `yaml:"AODdir"`
	} `yaml:"Setting"`
	GRPCServices struct {
		Host string `yaml:"Host"`
		Port string `yaml:"Port"`
	} `yaml:"GRPCServices"`
}

// CRC64 传入文件名计算CRC64  chunkSize分片大小, workers线程数 返回CRC64, 文件大小, 错误
func CRC64(filePath string, chunkSize int64, workers int) (uint64, int64, error) {
	f, err := os.OpenFile(filePath, os.O_RDONLY, 0644)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open file: %v", err)
	}
	defer f.Close()

	finformation, err := f.Stat()
	if err != nil {
		return 0, 0, fmt.Errorf("filed to get file information: %v", err)
	}

	fileSize := finformation.Size()
	if fileSize == 0 {
		return 0, finformation.Size(), err
	}

	chunks := int((fileSize + chunkSize - 1) / chunkSize)
	if workers > chunks {
		workers = chunks
	}

	table := crc64.MakeTable(crc64.ECMA)
	var wg sync.WaitGroup
	results := make([]uint64, chunks) // 使用切片存储结果，保持顺序
	workCh := make(chan int, chunks)
	errCh := make(chan error, 1)
	var hasError bool
	var mu sync.Mutex // 用于保护hasError

	// 启动worker池
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunkIndex := range workCh {
				offset := int64(chunkIndex) * chunkSize
				size := chunkSize
				if offset+size > fileSize {
					size = fileSize - offset
				}

				buf := make([]byte, size)
				n, err := f.ReadAt(buf, offset)
				if err != nil && err != io.EOF {
					mu.Lock()
					if !hasError {
						errCh <- fmt.Errorf("读取文件块 %d 失败: %v", chunkIndex, err)
						hasError = true
					}
					mu.Unlock()
					return
				}

				// 计算当前块的CRC
				sum := crc64.Checksum(buf[:n], table)

				// 直接存储到结果切片的对应位置
				results[chunkIndex] = sum
			}
		}()
	}

	// 分发任务
	go func() {
		for i := 0; i < chunks; i++ {
			mu.Lock()
			if hasError {
				mu.Unlock()
				break
			}
			mu.Unlock()
			workCh <- i
		}
		close(workCh)
	}()

	// 等待完成
	go func() {
		wg.Wait()
		close(errCh) // 关闭错误通道表示所有工作完成
	}()

	// 处理错误
	if err := <-errCh; err != nil {
		return 0, finformation.Size(), err
	}

	// 按顺序合并所有块的CRC值
	finalCRC := uint64(0)
	for i := 0; i < chunks; i++ {
		sum := results[i]
		finalCRC = crc64.Update(finalCRC, table, []byte{
			byte(sum >> 56), byte(sum >> 48), byte(sum >> 40), byte(sum >> 32),
			byte(sum >> 24), byte(sum >> 16), byte(sum >> 8), byte(sum),
		})
	}

	return finalCRC, finformation.Size(), nil
}

// RemoveDuplicates 使用泛型实现的slice元素去重
func RemoveDuplicates[T comparable](source []T) []T {
	var duplicates = make(map[T]struct{})
	var response = make([]T, 0)
	for _, value := range source {
		duplicates[value] = struct{}{}
	}
	for key := range duplicates {
		response = append(response, key)
	}
	return response
}

func Changefiletime(fp string, munix, cunix int64) error {
	var (
		handle   syscall.Handle
		uint16fp *uint16
	)
	// 转换文件路径为UTF-16指针
	if uint16fp, err = syscall.UTF16PtrFromString(fp); err != nil {
		return err
	}

	// 打开文件获取句柄
	if handle, err = syscall.CreateFile(
		uint16fp,
		syscall.FILE_WRITE_ATTRIBUTES,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	); err != nil {
		return err
	}

	ParseWindowsTime := func(t time.Time) syscall.Filetime {
		return syscall.NsecToFiletime(t.UnixNano())
	}
	Ctime := ParseWindowsTime(time.Unix(cunix, 0))
	Mtime := ParseWindowsTime(time.Unix(munix, 0))
	Rtime := ParseWindowsTime(time.Now())
	defer syscall.CloseHandle(handle)
	return syscall.SetFileTime(handle, &Ctime, &Rtime, &Mtime)
}

func (the *Processprintstruct) Initialization() {
	const (
		KB = 1 << 10 // 1024
		MB = 1 << 20 // 1048576
		GB = 1 << 30 // 1073741824
		TB = 1 << 40 // 1099511627776
	)

	// 使用无表达式的switch进行范围判断
	switch {
	case the.Size < KB:
		the.conversion = 1
		the.processspri = goPrint.NewBar(the.Size)
		the.processspri.SetNotice("(Bytes)")
	case the.Size < MB:
		the.conversion = KB
		the.processspri = goPrint.NewBar(the.Size / KB)
		the.processspri.SetNotice("(KB)")
	case the.Size < GB:
		the.conversion = MB
		the.processspri = goPrint.NewBar(the.Size / MB)
		the.processspri.SetNotice("(MB)")
	case the.Size < TB:
		the.conversion = MB
		the.processspri = goPrint.NewBar(the.Size / MB)
		the.processspri.SetNotice("(MB)")

	default:
		the.conversion = TB
		the.processspri = goPrint.NewBar(the.Size / GB)
		the.processspri.SetNotice("(TB)")
	}

	the.processspri.SetGraph(`=`)
	the.processspri.SetEnds("{", "}")
}

func (the *Processprintstruct) ProcessPrint(size int) {
	the.processspri.PrintBar(size / the.conversion)
}

func GetIP() string {
	conn, err := net.Dial("udp", "10.60.203.50:80")
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	}(conn)
	return conn.LocalAddr().String()
}

func (the *Work) Run() {
	the.Workchannel <- struct{}{}
}

func (the *Work) Done() {
	<-the.Workchannel
}

func (the *Work) IsWorking() bool {
	return len(the.Workchannel) != 0
}
