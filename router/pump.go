package router

func init() {
	pump := &LogsAndStatsPump{
		pumps:  make(map[string]*containerPump),
		routes: make(map[chan *update]struct{}),
	}
	LogRouters.Register(pump, "pump")
	Jobs.Register(pump, "pump")
}
