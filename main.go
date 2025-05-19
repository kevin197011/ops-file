package main

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
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
	DatePath     string // 存储日期路径
}

var fileMap = make(map[string]FileInfo)

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

		// 记录文件信息
		fileMap[newFileName] = FileInfo{
			OriginalName: file.Filename,
			ExpireTime:   time.Now().AddDate(0, 0, expireDays),
			DatePath:     datePath,
		}

		// 返回下载链接
		downloadURL := fmt.Sprintf("/download/%s", newFileName)
		c.JSON(http.StatusOK, gin.H{
			"message": "File uploaded successfully",
			"url":     downloadURL,
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
		c.FileAttachment(filePath, fileInfo.OriginalName)
	})

	// 启动服务器
	r.Run(":8080")
}
