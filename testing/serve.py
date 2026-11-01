# -*- coding: utf-8 -*-
"""
PNA 测试页面 - 极简静态托管服务器
================================================================
用途：把本目录（含 pna-test.html）挂到公网/局域网服务器上，供现场
      浏览器通过 http://<服务器IP>:<端口>/pna-test.html 访问。

特点：
  - 只用 Python 标准库，零第三方依赖（pip 都不需要）。
  - 同时兼容 Python 2.7 和 Python 3.x，自动适配。
  - 不依赖系统已装任何 Web 服务器（nginx/apache 都不需要）。

用法：
  python serve.py            # 默认监听 0.0.0.0:8099
  python serve.py 8002       # 指定端口
  python serve.py 8002 0.0.0.0

然后现场浏览器访问：  http://<本机公网IP>:8002/pna-test.html
按 Ctrl+C 停止。
================================================================
"""

import sys
import os

# ---------------------------------------------------------------
# 1. 启动自检：打印 Python 版本，确认运行环境
# ---------------------------------------------------------------
PY2 = (sys.version_info[0] == 2)
PY3 = (sys.version_info[0] == 3)

print("=" * 60)
print("PNA 测试页面托管服务器")
print("Python 版本: %s" % sys.version.split()[0]
      + ("  (Python 2 兼容模式)" if PY2 else "  (Python 3 模式)"))
print("=" * 60)

if not (PY2 or PY3):
    sys.stderr.write("不支持的 Python 版本，请使用 Python 2.7 或 Python 3.x\n")
    sys.exit(1)

# ---------------------------------------------------------------
# 2. 解析参数：端口 + 绑定地址
# ---------------------------------------------------------------
port = 8099
bind = "0.0.0.0"
if len(sys.argv) >= 2:
    try:
        port = int(sys.argv[1])
    except ValueError:
        sys.stderr.write("端口必须是数字，例如: python serve.py 8002\n")
        sys.exit(1)
if len(sys.argv) >= 3:
    bind = sys.argv[2]

# 切到脚本所在目录，确保能找到 pna-test.html
script_dir = os.path.dirname(os.path.abspath(__file__))
os.chdir(script_dir)

html_path = os.path.join(script_dir, "pna-test.html")
if not os.path.exists(html_path):
    sys.stderr.write("警告: 当前目录未找到 pna-test.html (%s)\n" % script_dir)
    sys.stderr.write("      请把 serve.py 和 pna-test.html 放在同一目录。\n")

# ---------------------------------------------------------------
# 3. 兼容两版本的 HTTP 服务器导入
# ---------------------------------------------------------------
if PY3:
    from http.server import SimpleHTTPRequestHandler
    try:
        from http.server import ThreadingHTTPServer as _Server
    except ImportError:
        # 老的 Python3 没有 ThreadingHTTPServer
        from http.server import HTTPServer as _Server
else:
    # Python 2.7
    from SimpleHTTPServer import SimpleHTTPRequestHandler  # noqa
    from BaseHTTPServer import HTTPServer as _Server        # noqa


class Handler(SimpleHTTPRequestHandler):
    """在标准静态服务基础上加防缓存头，确保现场每次拿到最新页面。"""

    def end_headers(self):
        self.send_header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
        self.send_header("Pragma", "no-cache")
        try:
            SimpleHTTPRequestHandler.end_headers(self)
        except Exception:
            # 极端情况下父类已写过 header，忽略
            pass

    def log_message(self, fmt, *args):
        # 精简日志：只打印访问的路径
        try:
            sys.stdout.write("[访问] %s - %s\n" % (self.address_string(), fmt % args))
        except Exception:
            pass


def guess_lan_ips():
    """尽力猜测本机可访问的 IP，仅用于提示。"""
    ips = []
    try:
        import socket
        hostname = socket.gethostname()
        try:
            for info in socket.getaddrinfo(hostname, None):
                ip = info[4][0]
                if ip not in ips and ":" not in ip:
                    ips.append(ip)
        except Exception:
            pass
        # 通过 UDP 连接探测出站网卡 IP（不会真的发包）
        try:
            s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            s.connect(("8.8.8.8", 80))
            ip = s.getsockname()[0]
            if ip not in ips:
                ips.append(ip)
            s.close()
        except Exception:
            pass
    except Exception:
        pass
    return ips


def main():
    try:
        httpd = _Server((bind, port), Handler)
    except Exception as e:
        sys.stderr.write("启动失败（端口可能被占用）: %s\n" % e)
        sys.exit(1)

    print("")
    print("服务已启动，监听 %s:%d" % (bind, port))
    print("现场浏览器请访问以下任一地址 + /pna-test.html ：")
    for ip in guess_lan_ips():
        print("    http://%s:%d/pna-test.html" % (ip, port))
    print("    (若上面的 IP 不是公网地址，请用服务器真实公网 IP 替换)")
    print("")
    print("按 Ctrl+C 停止服务。")
    print("-" * 60)

    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\n正在停止服务...")
        try:
            httpd.shutdown()
        except Exception:
            pass
        print("已停止。")


if __name__ == "__main__":
    main()
