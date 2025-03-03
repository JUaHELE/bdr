// server

package main

type Server struct {
	Config        *Config
	Listener net.Listener
	Conn          net.Conn
	Encoder       *gob.Encoder
	Decoder       *gob.Decoder
	TargetDevFd   *os.File
	TermChan chan struct{}
}

func NewServer(cfg *Config) (*Server, error) {
	targetDeviceFd, err := os.OpenFile(cfg.TargetDevPath, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}
	
	address := fmt.Sprintf("%s:%d", cfg.IpAddress, cfg.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		targetDeviceFd.Close()
		return nil, fmt.Errorf("failed to listen on port %d: %w", cfg.Port, err)
	}

	

}

func (s *Server) Close() {
	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}
	if s.Listener != nil {
		s.Listener.Close()
	}
	if s.TargetDevFd != nil {
		s.TargetDevFd.Close()
	}
}


func main() {
	cfg := NewConfig()

	err := ValidateArgs(&cfg.TargetDevPath, &cfg.Port, &cfg.IpAddress)
	if err != nil {
		log.Fatalf("Invalid arguments: %v", err)
	}

	server, err := NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}
	defer server.Close()

	networking.RegisterGobPackets()

	// server.Run()
}
