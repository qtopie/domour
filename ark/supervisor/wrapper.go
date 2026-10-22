package supervisor

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// WrapCommandWithVProxy 根据配置判断是否需要使用 vproxy 包装目标执行命令。
// 若开启且系统中存在 vproxy（或指定了 vproxyBin），则生成包裹命令：
//
//	vproxy <targetBin> <targetArgs...>
//
// 从而确保子进程在 Linux 下自动被移入 /sys/fs/cgroup/vproxy 并继承透明代理拦截环境。
// 若未开启或系统无 vproxy，则安全降级为原生命令执行。
func WrapCommandWithVProxy(ctx context.Context, targetBin string, targetArgs []string, opts LaunchOptions) (*exec.Cmd, func()) {
	cleanup := func() {}

	needVProxy := opts.UseVProxy || os.Getenv("DOMOUR_USE_VPROXY") == "true"
	if !needVProxy {
		cmd := exec.CommandContext(ctx, targetBin, targetArgs...)
		if opts.Workspace != "" {
			cmd.Dir = opts.Workspace
		}
		applyEnv(cmd, opts.Env)
		return cmd, cleanup
	}

	vproxyBin := opts.VProxyBin
	if vproxyBin == "" {
		if path, err := exec.LookPath("vproxy"); err == nil {
			vproxyBin = path
		}
	} else {
		if _, err := os.Stat(vproxyBin); err != nil {
			if path, err := exec.LookPath(vproxyBin); err == nil {
				vproxyBin = path
			} else {
				vproxyBin = ""
			}
		}
	}

	// 系统未找到 vproxy 则优雅降级为直连
	if vproxyBin == "" {
		cmd := exec.CommandContext(ctx, targetBin, targetArgs...)
		if opts.Workspace != "" {
			cmd.Dir = opts.Workspace
		}
		applyEnv(cmd, opts.Env)
		return cmd, cleanup
	}

	wrapperArgs := append([]string{targetBin}, targetArgs...)
	cmd := exec.CommandContext(ctx, vproxyBin, wrapperArgs...)
	if opts.Workspace != "" {
		cmd.Dir = opts.Workspace
	}
	applyEnv(cmd, opts.Env)
	ensureProxyEnv(cmd)

	return cmd, cleanup
}

func applyEnv(cmd *exec.Cmd, env map[string]string) {
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	if len(env) == 0 {
		return
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
}

func ensureProxyEnv(cmd *exec.Cmd) {
	hasHTTPProxy := false
	for _, e := range cmd.Env {
		lower := strings.ToLower(e)
		if strings.HasPrefix(lower, "http_proxy=") || strings.HasPrefix(lower, "https_proxy=") {
			hasHTTPProxy = true
			break
		}
	}
	if !hasHTTPProxy {
		proxyURL := os.Getenv("HTTP_PROXY")
		if proxyURL == "" {
			proxyURL = os.Getenv("http_proxy")
		}
		if proxyURL == "" {
			proxyURL = "http://127.0.0.1:8118"
		}
		cmd.Env = append(cmd.Env,
			"http_proxy="+proxyURL,
			"https_proxy="+proxyURL,
			"HTTP_PROXY="+proxyURL,
			"HTTPS_PROXY="+proxyURL,
		)
	}
}
