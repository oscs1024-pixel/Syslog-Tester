package templates

type Template struct{ Name, Description, Message string }

func All() []Template {
	return []Template{
		{Name: "Linux SSH 登录失败", Description: "sshd 认证失败事件", Message: "Failed password for invalid user admin from 192.0.2.10 port 52344 ssh2"},
		{Name: "防火墙阻断", Description: "网络访问被拒绝", Message: "action=deny src=192.0.2.10 dst=198.51.100.20 src_port=52418 dst_port=443 proto=TCP"},
		{Name: "Linux sudo", Description: "sudo 权限操作事件", Message: "sudo: user1 : TTY=pts/0 ; PWD=/home/user1 ; USER=root ; COMMAND=/usr/bin/id"},
		{Name: "交换机端口变化", Description: "设备端口状态变更", Message: "Interface GigabitEthernet0/1 changed state to down"},
	}
}
