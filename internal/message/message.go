package message

import (
	"fmt"

	"github.com/go-telegram/bot/models"
)

// Callback data constants.
const (
	CbDnWhole     = "dn_whole"
	CbStickerInfo = "sticker_info"
)

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func StartMessage() string {
	return `<b>Telegram Sticker Downloader</b>

<b>Usage:</b>
• Send a <b>sticker</b> → download as PNG/GIF
• Send a <b>sticker set link</b> → download the whole set
• Send a <b>GIF</b> → convert and download

<b>FAQ:</b>
• Static stickers export as PNG, animated/video as GIF
• Whole set downloads are sent as ZIP
• Use /info to view sticker set details

<b>使用方法:</b>
• 发送<b>贴纸</b> → 自动下载 PNG/GIF
• 发送<b>贴纸包链接</b> → 下载整个贴纸包
• 发送<b>GIF</b> → 转换并下载

<b>常见问题:</b>
• 静态贴纸导出为 PNG，动态/视频贴纸导出为 GIF
• 整包下载以 ZIP 格式发送
• 使用 /info 查看贴纸包详情`
}

func AboutMessage(version string) string {
	return fmt.Sprintf(`<b>Telegram Sticker Downloader</b>

A lightweight bot for downloading Telegram stickers as PNG/GIF files.
一个轻量的 Telegram 贴纸下载机器人。

<b>Features / 功能:</b>
• Download single stickers as PNG or GIF / 单张贴纸下载为 PNG 或 GIF
• Download entire sticker sets as ZIP / 整包下载为 ZIP
• View sticker set details / 查看贴纸包详情
• GIF conversion / GIF 格式转换

<b>Privacy / 隐私:</b>
This bot collects anonymized usage statistics only (SHA256-hashed user IDs, no raw IDs stored). Sticker files are processed temporarily and deleted immediately.
本 Bot 仅收集匿名使用统计（SHA256 哈希用户 ID，不存储原始 ID）。贴纸文件仅临时处理后立即删除。

<b>Version:</b> <code>%s</code>
<b>License:</b> MIT
<b>GitHub:</b> https://github.com/xjoker/x_sticker_bot

/start`, version)
}

// ---------------------------------------------------------------------------
// Download flow
// ---------------------------------------------------------------------------

func OfferWholeSetDownload() (string, *models.InlineKeyboardMarkup) {
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "Download whole set / 下载整个贴纸包", CallbackData: CbDnWhole}},
			{{Text: "Set info / 贴纸包详情", CallbackData: CbStickerInfo}},
		},
	}
	return "Need more? / 需要更多操作吗?", kb
}

func ProcessStarted() string {
	return "Processing, please wait... / 处理中，请稍候..."
}

// DownloadProgress returns a progress message for sticker set downloads.
func DownloadProgress(done, total int) string {
	return fmt.Sprintf("Downloading stickers... %d/%d\n下载贴纸中... %d/%d", done, total, done, total)
}

// PartialDownload notifies that only some stickers were downloaded successfully.
func PartialDownload(succeeded, total int) string {
	return fmt.Sprintf("Downloaded %d/%d stickers. Some failed, sending available ones.\n已下载 %d/%d 张贴纸，部分失败，发送已成功的文件。",
		succeeded, total, succeeded, total)
}

// ---------------------------------------------------------------------------
// Errors / Warnings
// ---------------------------------------------------------------------------

func FatalError(err error) string {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	return "<b>Error, please try again. / 处理失败，请重试。</b> /start\n\n<code>" + errMsg + "</code>"
}

func ServerBusy() string {
	return "Server is at full capacity, please try again later.\n服务器满负荷运行中，请稍后再试。"
}

func UserBusy() string {
	return "You already have a task in progress, please wait.\n您已有任务正在处理中，请等待完成。"
}

func SendStickerHint() string {
	return "Send a sticker or sticker set link to download.\n请发送贴纸或贴纸包链接来下载。\n/start"
}
