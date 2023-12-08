# tcp
The project is based on the Go language and implements the TCP protocol using network card drivers.   
To run this project, you need to implement the relevant driver interfaces.
> **Note**  
> Using TUN requires adding a routing table.

## Usage
```go
// main.go
package main

import (
	"github.com/hhzhhzhhz/tcp"
	"github.com/hhzhhzhhz/tcp/log"
	"time"
)

func main() {
	dr, err := tcp.NewDriveWrapper(nil, &tcp.Option{})
	if err != nil {
		log.Logger().Error(err.Error())
		return
	}
	c, err := tcp.DailTcpV("src_mac", "dst_mac", "192.168.0.1:8888", "192.168.0.2:9999", dr, 10*time.Second)
	if err != nil {
		log.Logger().Error(err.Error())
		return
	}
	_, err := c.Write([]byte("hello world."))
	if err != nil {
		log.Logger().Error(err.Error())
		return
	}
	buf := make([]byte, 1024)
	for {
		n, err := c.Read(buf)
		if err != nil {
			continue
		}
		fmt.Println(string(buf[:n]))
	}
}
```

## Reward and support
<center>
    <img style="border-radius: 0.3125em;
    box-shadow: 0 2px 4px 0 rgba(34,36,38,.12),0 2px 10px 0 rgba(34,36,38,.08);" 
    src="alipay.jpg" width=30% align="center">
    <br>
    <div style="color:orange; border-bottom: 1px solid #d9d9d9;
    display: inline-block;
    color: #999;
    padding: 2px;">Alipay</div>
</center>

