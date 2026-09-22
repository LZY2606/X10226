package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"tgareview/internal/casepack"
	"tgareview/internal/domain"
	"tgareview/internal/service"
	"tgareview/internal/store"
)

func main() {
	dir := "/tmp"
	st, _ := store.Open(filepath.Join(dir, "dbg2.db"))
	defer st.Close()
	cur := time.Date(2026,1,1,0,0,0,0,time.UTC)
	svc := service.New(st, func() time.Time { t := cur; cur = cur.Add(time.Minute); return t })
	r := casepack.NewRunner(svc)
	ctx := context.Background()
	pid, e := r.CreateBaseline(ctx, "dbg2"); if e != nil { panic(e) }
	v, err := svc.HeadView(ctx, pid); if err != nil { panic(err) }
	var sid string
	for _, s := range v.View.Stages { if s.Rejected == nil { sid = s.ID } }
	fmt.Println("stage", sid)
	_,_,e1 := svc.Decide(ctx, pid, service.DecideRequest{Kind: domain.DecAcceptStage, Target:"stage", TargetID:sid, Actor:"a"})
	fmt.Println("accept err:", e1)
	_,cid,e2 := svc.Decide(ctx, pid, service.DecideRequest{Kind: domain.DecRejectStage, Target:"stage", TargetID:sid, Actor:"b"})
	fmt.Println("reject err:", e2, "cid:", cid)
	evs,_ := svc.RawEvents(ctx, pid)
	for _, e := range evs { if e.Type==domain.EvDecision { fmt.Printf("decision seq=%d id=%s\n", e.Seq, e.EventID) } }
}
