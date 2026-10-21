#!/usr/bin/env bash
# A13 · deb 打包脚本（流程来源：build/linux/{x86,arm}/deepin25/README.md）
#
# 一条命令完成：wails 构建（prod + dev 两个二进制）→ 依赖探测 → deb 目录树 → 图标多尺寸 → dpkg-deb 打包。
# 必须在 Linux 构建机执行（依赖 wails / node / dpkg-deb / ImageMagick / binutils（readelf））。
#
# 用法：
#   scripts/build-deb.sh                 # 默认 amd64，正式 + dev 各出一包
#   scripts/build-deb.sh arm64           # 交叉打 arm64（需 aarch64 工具链，见 arm README）
#   VERSION=1.2.3 scripts/build-deb.sh
#   SKIP_BUILD=1 scripts/build-deb.sh    # 复用 build/bin 下已有二进制，只重打包
set -euo pipefail
cd "$(dirname "$0")/.."

ARCH="${1:-amd64}"
case "$ARCH" in
amd64)
  WAILS_PLATFORM="linux/amd64"
  DPKG_ARCH="amd64"
  ;;
arm64)
  WAILS_PLATFORM="linux/arm64"
  DPKG_ARCH="arm64"
  ;;
*)
  echo "不支持的架构: $ARCH（amd64|arm64）" >&2
  exit 1
  ;;
esac

# 版本号：显式 VERSION > wails.json info.productVersion > 兜底
VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(node -p "try{require('./wails.json').info.productVersion||''}catch(e){''}" 2>/dev/null || true)"
fi
[ -n "$VERSION" ] || VERSION="0.0.0-dev"

# ---- 版本合法性校验（deb 侧 L-1，提前拦截避免 dpkg-deb 末尾报晦涩错误）----
# dpkg 版本格式：[epoch:]upstream[-revision]；upstream 必须以数字开头且含数字；
# '-' 后的 revision 若含非数字（如 0.1.0-dev），dpkg 判为非法。
# 这里把非法 revision 用 '~' 连接（0.1.0-dev → 0.1.0~dev，'~' 排序低于正式版，语义=预发布）。
case "$VERSION" in
'' | *[!0-9A-Za-z.~+-]*)
  echo "VERSION='$VERSION' 含 dpkg 非法字符（仅允许 字母 数字 . ~ + -）" >&2
  exit 1
  ;;
esac
base="${VERSION%%-*}"
rev="${VERSION#*-}"
if [ "$VERSION" != "$base" ] && printf '%s' "$rev" | grep -qE '[^0-9]'; then
  NEW="${base}~${rev}"
  echo "⚠️  版本号 '$VERSION' 的修订段含非数字，dpkg 判非法；已自动规范化为 '$NEW'（'~'=预发布，排序低于正式版）。" >&2
  VERSION="$NEW"
fi
case "$VERSION" in
[0-9]*) : ;;
*)
  echo "VERSION='$VERSION' 必须以数字开头（dpkg 版本要求）" >&2
  exit 1
  ;;
esac

BIN="build/bin"
command -v dpkg-deb >/dev/null || {
  echo "缺少 dpkg-deb（deb 打包必需）" >&2
  exit 1
}
mkdir -p "$BIN"

# ---- 1) 构建二进制（prod / dev 两份；SKIP_BUILD=1 可复用已有产物）----
if [ "${SKIP_BUILD:-0}" != "1" ]; then
  command -v wails >/dev/null || {
    echo "缺少 wails CLI（或用 SKIP_BUILD=1 复用已有二进制）" >&2
    exit 1
  }
  wails build -platform "$WAILS_PLATFORM" -clean -o api-doc-client
  wails build -platform "$WAILS_PLATFORM" -tags devbuild -o api-doc-client-dev
fi
# dev 二进制名兼容：SKIP_BUILD 时旧产物可能是 api-doc-client-dev-linux-amd64
if [ ! -x "$BIN/api-doc-client-dev" ] && [ -x "$BIN/api-doc-client-dev-linux-amd64" ]; then
  cp -f "$BIN/api-doc-client-dev-linux-amd64" "$BIN/api-doc-client-dev"
fi
[ -x "$BIN/api-doc-client" ] || {
  echo "未找到 $BIN/api-doc-client，请先构建" >&2
  exit 1
}
[ -x "$BIN/api-doc-client-dev" ] || {
  echo "未找到 $BIN/api-doc-client-dev" >&2
  exit 1
}

# ---- 2) 依赖探测：Depends 必须与二进制实际链接的 WebKit soname 一致 ----
# apt 对 "A | B" 只会装其中一个；若装的是另一种 soname，包能装上但程序启动失败。
# 用 readelf 读 DT_NEEDED（对交叉编译的 arm64 ELF 同样有效），写精确依赖。
need_webkit() {
  local bin="$1" soname
  command -v readelf >/dev/null || {
    echo "缺少 readelf（binutils），无法探测 WebKit 依赖" >&2
    return 1
  }
  soname="$(readelf -d "$bin" 2>/dev/null | grep -oE 'libwebkit2gtk-4\.[01]\.so\.[0-9]+' | head -n1 || true)"
  case "$soname" in
  libwebkit2gtk-4.1.so.0) echo 'libwebkit2gtk-4.1-0' ;;
  libwebkit2gtk-4.0.so.37) echo 'libwebkit2gtk-4.0-37' ;;
  *)
    echo "无法从 $bin 探测 WebKit soname（readelf 未命中 4.0/4.1）" >&2
    return 1
    ;;
  esac
}

WEBKIT_DEP="$(need_webkit "$BIN/api-doc-client")" || exit 1
DEV_WEBKIT_DEP="$(need_webkit "$BIN/api-doc-client-dev")" || exit 1
if [ "$WEBKIT_DEP" != "$DEV_WEBKIT_DEP" ]; then
  echo "prod/dev 的 WebKit 依赖不一致：$WEBKIT_DEP vs $DEV_WEBKIT_DEP（请用相同 tags 构建）" >&2
  exit 1
fi
echo "WebKit 依赖：$WEBKIT_DEP（按二进制实际 soname 生成）"

# ---- 3) 图标缩放（源 1024见 README §5；dev 图标缺失时回落正式图标，不阻塞）----
SCALE=""
for cand in magick convert; do
  if command -v "$cand" >/dev/null; then
    SCALE="$cand"
    break
  fi
done
[ -n "$SCALE" ] || {
  echo "需要 ImageMagick（magick 或 convert）生成多尺寸图标" >&2
  exit 1
}

SRC_ICON="build/appicon.png"
[ -f "$SRC_ICON" ] || {
  echo "缺少 $SRC_ICON（正式图标源）" >&2
  exit 1
}
DEV_ICON="internal/appicon/appicon_dev.png"
[ -f "$DEV_ICON" ] || DEV_ICON="$SRC_ICON"

gen_icons() { # $1=源 png  $2=图标主题名（= 包名）  $3=pkg 根
  local src="$1" name="$2" pkg="$3" sz dir
  for sz in 16 32 48 64 128 256 512; do
    dir="$pkg/usr/share/icons/hicolor/${sz}x${sz}/apps"
    mkdir -p "$dir"
    "$SCALE" "$src" -resize "${sz}x${sz}" "$dir/$name.png"
  done
}

# WM_CLASS 采用 README §4「固定二进制名」方案：GTK 的 WM_CLASS 取自可执行文件名，
# 因此 .desktop 的 StartupWMClass 与二进制名一致；dev 与 prod 不同名，dock 不并组。
make_pkg() { # $1=pkg 根  $2=包名  $3=显示名  $4=图标源  $5=可执行文件主干名
  local pkg="$1" package="$2" disp="$3" icon_src="$4" base="$5"
  rm -rf "$pkg"
  mkdir -p "$pkg/DEBIAN" "$pkg/usr/bin" "$pkg/usr/share/applications"
  cp "$BIN/$base" "$pkg/usr/bin/$base"
  chmod 755 "$pkg/usr/bin/$base"
  gen_icons "$icon_src" "$package" "$pkg"

  {
    echo "Package: $package"
    echo "Version: $VERSION"
    echo "Architecture: $DPKG_ARCH"
    echo "Maintainer: api-doc-go"
    echo "Section: utils"
    echo "Priority: optional"
    # Depends 由上面的 readelf 探测结果生成：必须与二进制实际链接的 soname 一致（L-2）
    echo "Depends: libgtk-3-0, $WEBKIT_DEP"
    # Installed-Size 单位 KiB，控制卸载后空间估算（L-5）；此刻二进制与图标已就位
    echo "Installed-Size: $(du -sk "$pkg" 2>/dev/null | cut -f1)"
    echo "Description: AI-native, local-first API client"
    echo " Markdown 风格接口文档协作工具的桌面客户端。"
  } >"$pkg/DEBIAN/control"

  {
    echo "[Desktop Entry]"
    echo "Type=Application"
    echo "Name=$disp"
    echo "GenericName=API Client"
    echo "Comment=AI-native、local-first 的 API 客户端"
    echo "Exec=/usr/bin/$base"
    echo "Icon=$package"
    echo "Terminal=false"
    echo "StartupWMClass=$base"
    echo "Categories=Development;Utility;"
    echo "StartupNotify=true"
  } >"$pkg/usr/share/applications/$package.desktop"

  # 缓存刷新失败改为告警而非静默吞掉（L-4）：桌面图标/启动项没刷新要能被看到，但不阻断安装/卸载
  printf '%s\n' '#!/bin/sh' \
    'update-desktop-database -q /usr/share/applications 2>/dev/null || echo "warning: update-desktop-database 刷新失败（不影响安装）" >&2' \
    'gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor 2>/dev/null || echo "warning: gtk-update-icon-cache 刷新失败（不影响安装）" >&2' \
    >"$pkg/DEBIAN/postinst"
  printf '%s\n' '#!/bin/sh' \
    'update-desktop-database -q /usr/share/applications 2>/dev/null || echo "warning: update-desktop-database 刷新失败（不影响卸载）" >&2' \
    >"$pkg/DEBIAN/postrm"
  chmod 755 "$pkg/DEBIAN/postinst" "$pkg/DEBIAN/postrm"
}

# ---- 4) 组树并打包（prod 与 dev 独立包名，可共存安装）----
deb_prod="$BIN/api-doc-client_${VERSION}_${DPKG_ARCH}.deb"
deb_dev="$BIN/api-doc-client-dev_${VERSION}_${DPKG_ARCH}.deb"

make_pkg "$BIN/pkg-prod" "api-doc-client" "API Doc Client" "$SRC_ICON" "api-doc-client"
make_pkg "$BIN/pkg-dev" "api-doc-client-dev" "API Doc Client Dev" "$DEV_ICON" "api-doc-client-dev"

dpkg-deb --build --root-owner-group "$BIN/pkg-prod" "$deb_prod"
dpkg-deb --build --root-owner-group "$BIN/pkg-dev" "$deb_dev"
rm -rf "$BIN/pkg-prod" "$BIN/pkg-dev"

echo
echo "完成："
echo "  $deb_prod"
echo "  $deb_dev"
echo "安装验证：sudo apt install ./$deb_prod && gtk-launch api-doc-client"
