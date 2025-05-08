package log

import (
	"github.com/gin-gonic/gin"
	"github.com/huyouba1/k8m/internal/dao"
	"github.com/huyouba1/k8m/pkg/comm/utils/amis"
	"github.com/huyouba1/k8m/pkg/models"
)

func ListShell(c *gin.Context) {
	params := dao.BuildParams(c)
	m := &models.ShellLog{}

	items, total, err := m.List(params)
	if err != nil {
		amis.WriteJsonError(c, err)
		return
	}
	amis.WriteJsonListWithTotal(c, total, items)
}

func ListOperation(c *gin.Context) {
	params := dao.BuildParams(c)
	m := &models.OperationLog{}

	items, total, err := m.List(params)
	if err != nil {
		amis.WriteJsonError(c, err)
		return
	}
	amis.WriteJsonListWithTotal(c, total, items)
}
