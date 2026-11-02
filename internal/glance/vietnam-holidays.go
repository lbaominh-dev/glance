package glance

import (
	"encoding/json"
	"math"
	"time"
)

// Vietnamese holidays use the Vietnamese lunar calendar (UTC+7), which can
// differ from other East Asian lunar calendars by a day.
const vietnamTimeZone = 7.0

type vietnamHoliday struct {
	month int
	day   int
	name  string
}

var vietnamSolarHolidays = []vietnamHoliday{
	{1, 1, "Tết Dương lịch"},
	{3, 8, "Ngày Quốc tế Phụ nữ"},
	{4, 30, "Ngày Giải phóng miền Nam"},
	{5, 1, "Quốc tế Lao động"},
	{7, 27, "Ngày Thương binh - Liệt sĩ"},
	{9, 2, "Quốc khánh"},
	{10, 20, "Ngày Phụ nữ Việt Nam"},
	{11, 20, "Ngày Nhà giáo Việt Nam"},
	{12, 22, "Ngày thành lập Quân đội Nhân dân"},
	{12, 25, "Giáng sinh"},
}

var vietnamLunarHolidays = []vietnamHoliday{
	{1, 1, "Tết Nguyên Đán"},
	{1, 2, "Mùng 2 Tết"},
	{1, 3, "Mùng 3 Tết"},
	{1, 15, "Tết Nguyên Tiêu"},
	{3, 10, "Giỗ Tổ Hùng Vương"},
	{5, 5, "Tết Đoan Ngọ"},
	{8, 15, "Tết Trung thu"},
}

// Generated once for a fixed range so the calendar stays useful when browsing
// neighbouring years without shipping a lunar calendar implementation to the
// browser.
var vietnamHolidaysJSON = func() string {
	const startYear = 2024
	const endYear = 2040

	holidays := make(map[string]string)

	add := func(date time.Time, name string) {
		key := date.Format("2006-01-02")

		if existing, ok := holidays[key]; ok {
			holidays[key] = existing + " · " + name
			return
		}

		holidays[key] = name
	}

	for year := startYear; year <= endYear; year++ {
		for _, holiday := range vietnamSolarHolidays {
			add(time.Date(year, time.Month(holiday.month), holiday.day, 0, 0, 0, 0, time.UTC), holiday.name)
		}

		for _, holiday := range vietnamLunarHolidays {
			day, month, lunarYear := lunarToSolar(holiday.day, holiday.month, year, vietnamTimeZone)
			add(time.Date(lunarYear, time.Month(month), day, 0, 0, 0, 0, time.UTC), holiday.name)
		}
	}

	encoded, err := json.Marshal(holidays)
	if err != nil {
		return "{}"
	}

	return string(encoded)
}()

// The following functions are a Go port of Hồ Ngọc Đức's Vietnamese lunar
// calendar implementation, based on Jean Meeus' "Astronomical Algorithms".
func julianDayFromDate(day, month, year int) int {
	a := (14 - month) / 12
	y := year + 4800 - a
	m := month + 12*a - 3

	jd := day + (153*m+2)/5 + 365*y + y/4 - y/100 + y/400 - 32045
	if jd < 2299161 {
		jd = day + (153*m+2)/5 + 365*y + y/4 - 32083
	}

	return jd
}

func julianDayToDate(jd int) (day, month, year int) {
	var a, b, c, d, e, m int

	if jd > 2299160 {
		a = jd + 32044
		b = (4*a + 3) / 146097
		c = a - (b*146097)/4
	} else {
		b = 0
		c = jd + 32082
	}

	d = (4*c + 3) / 1461
	e = c - (1461*d)/4
	m = (5*e + 2) / 153

	day = e - (153*m+2)/5 + 1
	month = m + 3 - 12*(m/10)
	year = b*100 + d - 4800 + m/10

	return day, month, year
}

func newMoon(k int) float64 {
	t := float64(k) / 1236.85
	t2 := t * t
	t3 := t2 * t
	dr := math.Pi / 180

	jd1 := 2415020.75933 + 29.53058868*float64(k) + 0.0001178*t2 - 0.000000155*t3
	jd1 += 0.00033 * math.Sin((166.56+132.87*t-0.009173*t2)*dr)

	m := 359.2242 + 29.10535608*float64(k) - 0.0000333*t2 - 0.00000347*t3
	mpr := 306.0253 + 385.81691806*float64(k) + 0.0107306*t2 + 0.00001236*t3
	f := 21.2964 + 390.67050646*float64(k) - 0.0016528*t2 - 0.00000239*t3

	c1 := (0.1734-0.000393*t)*math.Sin(m*dr) + 0.0021*math.Sin(2*dr*m)
	c1 -= 0.4068 * math.Sin(mpr*dr)
	c1 += 0.0161 * math.Sin(dr*2*mpr)
	c1 -= 0.0004 * math.Sin(dr*3*mpr)
	c1 += 0.0104 * math.Sin(dr*2*f)
	c1 -= 0.0051 * math.Sin(dr*(m+mpr))
	c1 -= 0.0074 * math.Sin(dr*(m-mpr))
	c1 += 0.0004 * math.Sin(dr*(2*f+m))
	c1 -= 0.0004 * math.Sin(dr*(2*f-m))
	c1 -= 0.0006 * math.Sin(dr*(2*f+mpr))
	c1 += 0.0010 * math.Sin(dr*(2*f-mpr))
	c1 += 0.0005 * math.Sin(dr*(2*mpr+m))

	var deltaT float64
	if t < -11 {
		deltaT = 0.001 + 0.000839*t + 0.0002261*t2 - 0.00000845*t3 - 0.000000081*t*t3
	} else {
		deltaT = -0.000278 + 0.000265*t + 0.000262*t2
	}

	return jd1 + c1 - deltaT
}

func sunLongitude(jdn float64) float64 {
	t := (jdn - 2451545.0) / 36525
	t2 := t * t
	dr := math.Pi / 180

	m := 357.52910 + 35999.05030*t - 0.0001559*t2 - 0.00000048*t*t2
	l0 := 280.46645 + 36000.76983*t + 0.0003032*t2

	dl := (1.914600-0.004817*t-0.000014*t2)*math.Sin(dr*m) +
		(0.019993-0.000101*t)*math.Sin(dr*2*m) +
		0.000290*math.Sin(dr*3*m)

	l := (l0 + dl) * dr
	l -= math.Pi * 2 * math.Floor(l/(math.Pi*2))

	return l
}

func sunLongitudeAtMidnight(dayNumber int, timeZone float64) int {
	return int(sunLongitude(float64(dayNumber)-0.5-timeZone/24) / math.Pi * 6)
}

func newMoonDay(k int, timeZone float64) int {
	return int(newMoon(k) + 0.5 + timeZone/24)
}

func lunarMonth11(year int, timeZone float64) int {
	off := julianDayFromDate(31, 12, year) - 2415021
	k := int(math.Floor(float64(off) / 29.530588853))
	nm := newMoonDay(k, timeZone)

	if sunLongitudeAtMidnight(nm, timeZone) >= 9 {
		nm = newMoonDay(k-1, timeZone)
	}

	return nm
}

func leapMonthOffset(a11 int, timeZone float64) int {
	k := int(math.Floor((float64(a11)-2415021.076998695)/29.530588853 + 0.5))
	last := 0
	i := 1
	arc := sunLongitudeAtMidnight(newMoonDay(k+i, timeZone), timeZone)

	for {
		last = arc
		i++
		arc = sunLongitudeAtMidnight(newMoonDay(k+i, timeZone), timeZone)
		if arc == last || i >= 14 {
			break
		}
	}

	return i - 1
}

func lunarToSolar(lunarDay, lunarMonth, lunarYear int, timeZone float64) (day, month, year int) {
	var a11, b11 int

	if lunarMonth < 11 {
		a11 = lunarMonth11(lunarYear-1, timeZone)
		b11 = lunarMonth11(lunarYear, timeZone)
	} else {
		a11 = lunarMonth11(lunarYear, timeZone)
		b11 = lunarMonth11(lunarYear+1, timeZone)
	}

	k := int(math.Floor(0.5 + (float64(a11)-2415021.076998695)/29.530588853))
	off := lunarMonth - 11
	if off < 0 {
		off += 12
	}

	if b11-a11 > 365 {
		leapOff := leapMonthOffset(a11, timeZone)
		leapMonth := leapOff - 2
		if leapMonth < 0 {
			leapMonth += 12
		}

		if off >= leapOff {
			off++
		}
	}

	monthStart := newMoonDay(k+off, timeZone)

	return julianDayToDate(monthStart + lunarDay - 1)
}
