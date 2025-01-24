/******** Peter Winzell (c), 3/25/24 *********************************************/

package grpcMgr

import (
	"context"
	"github.com/covesa/vissr/utils"
	"google.golang.org/grpc/stats"
)

type Handler struct {
}

func (h *Handler) TagRPC(context.Context, *stats.RPCTagInfo) context.Context {
	utils.Info.Printf("TagRPC")
	return context.Background()
}

// HandleRPC processes the RPC stats.
func (h *Handler) HandleRPC(context.Context, stats.RPCStats) {
	utils.Info.Printf("HandleRPC")
}

func (h *Handler) TagConn(context.Context, *stats.ConnTagInfo) context.Context {

	utils.Info.Printf("TagConn")
	return context.Background()
}

// HandleConn processes the Conn stats.
func (h *Handler) HandleConn(c context.Context, s stats.ConnStats) {
	switch s.(type) {
	case *stats.ConnEnd:
		utils.Info.Printf("GRPPC client disconnected %d", c.Value("user_counter"))
		c.Done()
	}
}
