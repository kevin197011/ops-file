package main

import (
	"crypto/md5"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

//go:embed templates
var templatesFS embed.FS

const (
	uploadDir    = "./uploads"
	maxFileSize  = 1 << 30 // 1GB
	expireDays   = 7
	cleanupHours = 24
)

type FileInfo struct {
	OriginalName string
	ExpireTime   time.Time
	DatePath     string
	MD5          string // 存储文件的 MD5 校验码
}

var fileMap = make(map[string]FileInfo)

// 计算文件的 MD5 校验码
func calculateMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// 验证文件完整性
func verifyFileIntegrity(filePath string, expectedMD5 string) bool {
	actualMD5, err := calculateMD5(filePath)
	if err != nil {
		log.Printf("Error calculating MD5 for %s: %v", filePath, err)
		return false
	}
	return actualMD5 == expectedMD5
}

func init() {
	// 创建上传目录
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatal("Failed to create upload directory:", err)
	}

	// 启动清理过期文件的协程
	go cleanupExpiredFiles()
}

// 获取日期目录路径
func getDatePath() string {
	now := time.Now()
	return filepath.Join(uploadDir, now.Format("2006-01-02"))
}

func cleanupExpiredFiles() {
	for {
		now := time.Now()
		expiredCount := 0
		errorCount := 0

		// 清理过期文件
		for id, info := range fileMap {
			if now.After(info.ExpireTime) {
				filePath := filepath.Join(info.DatePath, id)
				if err := os.Remove(filePath); err != nil {
					log.Printf("Error removing expired file %s: %v", filePath, err)
					errorCount++
					continue
				}
				delete(fileMap, id)
				expiredCount++

				// 检查并清理空目录
				dir := filepath.Dir(filePath)
				if isEmpty, err := isDirEmpty(dir); err == nil && isEmpty {
					if err := os.Remove(dir); err != nil {
						log.Printf("Error removing empty directory %s: %v", dir, err)
					}
				}
			}
		}

		if expiredCount > 0 || errorCount > 0 {
			log.Printf("Cleanup completed: %d files removed, %d errors", expiredCount, errorCount)
		}

		time.Sleep(time.Hour * cleanupHours)
	}
}

// 检查目录是否为空
func isDirEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}

func main() {
	r := gin.Default()

	// 使用嵌入的模板文件
	tmpl := template.Must(template.ParseFS(templatesFS, "templates/*"))
	r.SetHTMLTemplate(tmpl)

	// 设置静态文件目录
	r.Static("/uploads", "./uploads")

	// 首页
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	// 文件上传
	r.POST("/upload", func(c *gin.Context) {
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
			return
		}

		if file.Size > maxFileSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": "File too large"})
			return
		}

		// 生成唯一文件名
		fileID := uuid.New().String()
		fileExt := filepath.Ext(file.Filename)
		newFileName := fileID + fileExt

		// 获取日期目录
		datePath := getDatePath()
		if err := os.MkdirAll(datePath, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create date directory"})
			return
		}

		// 保存文件
		filePath := filepath.Join(datePath, newFileName)
		if err := c.SaveUploadedFile(file, filePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
			return
		}

		// 计算文件的 MD5 校验码
		md5sum, err := calculateMD5(filePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate file checksum"})
			return
		}

		// 记录文件信息
		fileMap[newFileName] = FileInfo{
			OriginalName: file.Filename,
			ExpireTime:   time.Now().AddDate(0, 0, expireDays),
			DatePath:     datePath,
			MD5:          md5sum,
		}

		// 生成下载链接和 curl 命令
		downloadURL := fmt.Sprintf("/download/%s", newFileName)
		host := c.Request.Host
		if host == "" {
			host = "localhost:8080"
		}
		fullURL := fmt.Sprintf("http://%s%s", host, downloadURL)
		curlCmd := fmt.Sprintf("curl -L -o \"%s\" \"%s\"", file.Filename, fullURL)

		// 返回下载链接、MD5 校验码和 curl 命令
		c.JSON(http.StatusOK, gin.H{
			"message": "File uploaded successfully",
			"url":     downloadURL,
			"md5":     md5sum,
			"curl":    curlCmd,
		})
	})

	// 文件下载
	r.GET("/download/:id", func(c *gin.Context) {
		fileID := c.Param("id")
		fileInfo, exists := fileMap[fileID]
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
			return
		}

		if time.Now().After(fileInfo.ExpireTime) {
			c.JSON(http.StatusGone, gin.H{"error": "File has expired"})
			return
		}

		filePath := filepath.Join(fileInfo.DatePath, fileID)

		// 验证文件完整性
		if !verifyFileIntegrity(filePath, fileInfo.MD5) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "File integrity check failed"})
			return
		}

		// 设置响应头，保持原始文件名和类型
		// 对文件名进行 URL 编码，确保中文和特殊字符正确显示
		encodedFilename := url.QueryEscape(fileInfo.OriginalName)
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", encodedFilename))
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Transfer-Encoding", "binary")
		c.Header("Expires", "0")
		c.Header("Cache-Control", "must-revalidate")
		c.Header("Pragma", "public")
		c.Header("X-File-MD5", fileInfo.MD5)

		c.File(filePath)
	})

	// 启动服务器
	r.Run(":8080")
}
