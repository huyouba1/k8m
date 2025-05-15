package cluster

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/duke-git/lancet/v2/slice"
	"github.com/gin-gonic/gin"
	"github.com/huyouba1/k8m/pkg/comm/utils"
	"github.com/huyouba1/k8m/pkg/comm/utils/amis"
	"github.com/huyouba1/k8m/pkg/service"
)

// 集群安装任务请求结构
// POST /admin/cluster/install
// GET  /admin/cluster/install/status?id=xxx

type ClusterInstallRequest struct {
	ClusterName string `json:"cluster_name"`
	K8sVersion  string `json:"k8s_version"`
	Hosts       []struct {
		IP       string `json:"ip"`
		Port     string `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Role     string `json:"role"` // master/node
	} `json:"hosts"`
}

type ClusterInstallStatus struct {
	Progress int    `json:"progress"` // 0-100
	Log      string `json:"log"`
	Status   string `json:"status"` // running/success/failed
}

var installTasks sync.Map // taskID -> *ClusterInstallStatus

func FileOptionList(c *gin.Context) {
	clusters := service.ClusterService().AllClusters()

	if len(clusters) == 0 {
		amis.WriteJsonData(c, gin.H{
			"options": make([]map[string]string, 0),
		})
		return
	}

	var fileNames []string
	for _, cluster := range clusters {
		fileNames = append(fileNames, cluster.FileName)
	}
	fileNames = slice.Unique(fileNames)
	var options []map[string]interface{}
	for _, fn := range fileNames {
		options = append(options, map[string]interface{}{
			"label": fn,
			"value": fn,
		})
	}

	amis.WriteJsonData(c, gin.H{
		"options": options,
	})
}

func Scan(c *gin.Context) {
	service.ClusterService().Scan()
	amis.WriteJsonData(c, "ok")
}

func Reconnect(c *gin.Context) {
	clusterBase64 := c.Param("cluster")
	clusterID, err := utils.DecodeBase64(clusterBase64)
	if err != nil {
		amis.WriteJsonError(c, err)
		return
	}
	go service.ClusterService().Connect(clusterID)
	amis.WriteJsonOKMsg(c, "已执行，请稍后刷新")
}
func Disconnect(c *gin.Context) {
	clusterBase64 := c.Param("cluster")
	clusterID, err := utils.DecodeBase64(clusterBase64)
	if err != nil {
		amis.WriteJsonError(c, err)
		return
	}
	service.ClusterService().Disconnect(clusterID)
	amis.WriteJsonOKMsg(c, "已执行，请稍后刷新")
}

func InstallCluster(c *gin.Context) {
	var req ClusterInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		amis.WriteJsonError(c, err)
		return
	}
	// 生成唯一任务ID
	taskID := fmt.Sprintf("%d", time.Now().UnixNano())
	installTasks.Store(taskID, &ClusterInstallStatus{Progress: 0, Log: "任务已创建", Status: "running"})

	go func(taskID string, req ClusterInstallRequest) {
		status := &ClusterInstallStatus{Progress: 10, Log: "准备安装环境...\n", Status: "running"}
		installTasks.Store(taskID, status)

		// 1. 创建临时目录
		tmpDir := filepath.Join(os.TempDir(), "k8s_install_"+taskID)
		os.MkdirAll(tmpDir, 0755)

		// 2. 生成 inventory 文件
		inventoryPath := filepath.Join(tmpDir, "inventory.ini")
		f, _ := os.Create(inventoryPath)
		defer f.Close()
		f.WriteString("[master]\n")
		for _, h := range req.Hosts {
			if h.Role == "master" {
				f.WriteString(fmt.Sprintf("%s ansible_ssh_user=%s ansible_ssh_pass=%s ansible_port=%s\n", h.IP, h.User, h.Password, h.Port))
			}
		}
		f.WriteString("\n[node]\n")
		for _, h := range req.Hosts {
			if h.Role == "node" {
				f.WriteString(fmt.Sprintf("%s ansible_ssh_user=%s ansible_ssh_pass=%s ansible_port=%s\n", h.IP, h.User, h.Password, h.Port))
			}
		}
		f.Sync()

		// 3. 生成 playbook.yml（简单示例，可替换为实际K8s安装playbook）
		playbookPath := filepath.Join(tmpDir, "playbook.yml")
		playbookContent := `
- hosts: all
  become: yes
  tasks:
    - name: Print hello
      debug:
        msg: "Hello from {{ inventory_hostname }}"
`
		os.WriteFile(playbookPath, []byte(playbookContent), 0644)

		status.Progress = 30
		status.Log += "生成Ansible inventory和playbook...\n"
		installTasks.Store(taskID, status)

		// 4. 调用 ansible-playbook
		cmd := exec.Command("ansible-playbook", "-i", inventoryPath, playbookPath)
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		_ = cmd.Start()

		// 5. 实时读取日志
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					status.Log += string(buf[:n])
					installTasks.Store(taskID, status)
				}
				if err != nil {
					if err == io.EOF {
						break
					}
					break
				}
			}
		}()
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stderr.Read(buf)
				if n > 0 {
					status.Log += string(buf[:n])
					installTasks.Store(taskID, status)
				}
				if err != nil {
					if err == io.EOF {
						break
					}
					break
				}
			}
		}()

		err := cmd.Wait()
		if err != nil {
			status.Progress = 100
			status.Log += fmt.Sprintf("\n安装失败: %v", err)
			status.Status = "failed"
		} else {
			status.Progress = 100
			status.Log += "\n安装完成！"
			status.Status = "success"
		}
		installTasks.Store(taskID, status)
	}(taskID, req)

	amis.WriteJsonData(c, gin.H{"task_id": taskID})
}

func InstallClusterStatus(c *gin.Context) {
	taskID := c.Query("id")
	if v, ok := installTasks.Load(taskID); ok {
		status := v.(*ClusterInstallStatus)
		amis.WriteJsonData(c, gin.H{
			"progress": status.Progress,
			"log":      status.Log,
			"status":   status.Status,
		})
	} else {
		amis.WriteJsonError(c, fmt.Errorf("任务不存在"))
	}
}
