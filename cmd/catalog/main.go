package main

import (
	"log"

	"github.com/xuri/excelize/v2"
)

func main() {
	f := excelize.NewFile()
	sheet := "routing"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		log.Fatal(err)
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{
		"دامنه گیت‌وی",
		"الگوی path",
		"سرویس مقصد",
		"پارامتر / هدر",
		"نمونه درخواست",
		"پاسخ انتظاری",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	rows := [][]string{
		{
			"map-gateway.sabzevar.ir",
			"/*",
			"http://192.168.1.19:7003 (map-api nginx)",
			"X-Forwarded-Gateway=map ؛ path و ?key= بدون تغییر؛ توکن gateway-core نگذارید",
			"curl -I https://map-gateway.sabzevar.ir:8004/styles/style.json?key=pk_...",
			"پاسخ map-api / Nest با Host بازنویسی‌شده",
		},
		{
			"map-gateway.sabzevar.ir",
			"/tiles/*",
			"http://192.168.1.19:7003/tiles/...",
			"کلید map-api با ?key=pk_... (نه X-API-Key؛ گیت‌وی آن هدر را حذف می‌کند)",
			"curl -I https://map-gateway.sabzevar.ir:8004/tiles/iran/0/0/0?key=pk_...",
			"کاشی از map-api → Martin/geo",
		},
		{
			"apisrv-gatewaylogin.sabzevar.ir",
			"/*",
			"https://apisrv.sabzevar.ir (قابل تغییر در پنل)",
			"حساس: Cookie و Authorization فوروارد؛ Location/Cookie به دامنه گیت‌وی",
			"curl -i https://apisrv-gatewaylogin.sabzevar.ir/auth/login",
			"پاسخ سرویس لاگین بدون افشای URL داخلی",
		},
		{
			"gateway-admin.sabzevar.ir",
			"/",
			"پنل مدیریت gateway-core",
			"لاگین ادمین محلی",
			"https://gateway-admin.sabzevar.ir/",
			"UI فارسی آمار و CRUD",
		},
		{
			"apisrv-gateway137.sabzevar.ir",
			"/* (آینده)",
			"از پنل اضافه شود",
			"فقط DNS + ردیف گیت‌وی؛ کد عوض نمی‌شود",
			"—",
			"—",
		},
	}
	for r, row := range rows {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	_ = f.SetColWidth(sheet, "A", "F", 42)
	if err := f.SaveAs("docs/routing-catalog.xlsx"); err != nil {
		log.Fatal(err)
	}
}
