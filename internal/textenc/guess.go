package textenc

import (
	"bytes"
	"sort"
	"strings"
)

// Guessing a legacy encoding.
//
// A legacy multibyte encoding accepts much of another's bytes (EUC-JP,
// EUC-KR, GB18030 and Big5 all take pairs of high bytes; GB18030, Code Page
// 949 and Windows-1252 decode almost anything), so a strict decode only rules
// encodings out. What decides is the text each remaining one yields:
//
//   - East Asian text is scored by how much of it is the language's frequent
//     characters — kana and common kanji for Japanese, the most frequent
//     syllables for Korean, the most frequent characters for simplified and
//     for traditional Chinese. Real text scores high; the bytes of another
//     encoding read as this one yield arbitrary characters and score low.
//     Japanese has no Hangul, Chinese neither kana nor Hangul.
//   - Western text (Windows-1252) has accented letters of the Western
//     European languages, in short runs, and typography that does not sit
//     inside a word. East Asian text read as Windows-1252 is runs of
//     symbols, C1 letters (Š, Ÿ) and quotes glued to ASCII letters; Central
//     European, Baltic or Turkish text is letters Windows-1252 has as symbols
//     (ł as ³, ż as ¿) or as Icelandic letters (ş as þ).
//
// An encoding is used when it alone reads the bytes, or when it reads them
// clearly better (minMargin) than every other encoding that reads them at
// all — and then only on enough text to tell (minEvidence). Anything else is
// undetermined: a few characters can be words in several encodings.

const (
	minEastAsianScore = 0.3 // share of the language's frequent characters
	minWesternScore   = 0.9 // share of runs of high bytes that read as Western
	minMargin         = 2.0 // the best score over the second best
	minEvidence       = 8   // non-ASCII characters to tell East Asian encodings apart
	minWesternRuns    = 3   // runs of high bytes to tell Windows-1252 from another
	sampleSize        = 1 << 16
)

var legacyNames = []string{"shift_jis", "euc-jp", "gb18030", "big5", "euc-kr", "windows-1252"}

type candidate struct {
	name     string
	score    float64
	evidence int // non-ASCII characters (East Asian) or runs (Western) scored
}

func guess(data []byte) Result {
	sample := scoringSample(data)
	var cands []candidate
	for _, name := range legacyNames {
		t, ok := decodeAs(name, sample)
		if !ok {
			continue
		}
		var c candidate
		if name == "windows-1252" {
			c = westernScore(sample, t)
		} else {
			c = eastAsianScore(name, t)
		}
		c.name = name
		cands = append(cands, c)
	}
	if len(cands) == 0 {
		return Result{Kind: Undetermined, Reason: "not UTF-8, and no supported encoding reads it"}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	best := cands[0]
	min := minEastAsianScore
	if best.name == "windows-1252" {
		min = minWesternScore
	}
	if best.score < min {
		return Result{Kind: Undetermined, Reason: "not UTF-8, and no supported encoding reads it as plausible text"}
	}
	if len(cands) > 1 {
		names := make([]string, len(cands))
		for i, c := range cands {
			names[i] = c.name
		}
		need := minEvidence
		if best.name == "windows-1252" {
			need = minWesternRuns
		}
		if best.evidence < need {
			return Result{Kind: Undetermined, Reason: "not UTF-8, and too short to tell " + strings.Join(names, ", ") + " apart"}
		}
		if best.score < minMargin*cands[1].score {
			return Result{Kind: Undetermined, Reason: "not UTF-8, and it reads as " + strings.Join(names[:2], " or ")}
		}
	}
	t, ok := decodeAs(best.name, data)
	if !ok {
		return Result{Kind: Undetermined, Reason: "not UTF-8, and not valid " + best.name + " throughout"}
	}
	return text(t, best.name)
}

// scoringSample is the part of data the encodings are scored on: all of it,
// or, of a larger file, its lines with a byte above ASCII — what tells
// encodings apart — up to sampleSize bytes. Lines end at a line feed, a byte
// no supported legacy encoding uses inside a character, so no character is
// cut.
func scoringSample(data []byte) []byte {
	if len(data) <= sampleSize {
		return data
	}
	var sample []byte
	for rest := data; len(rest) > 0 && len(sample) < sampleSize; {
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i+1]
		}
		rest = rest[len(line):]
		if hasHighByte(line) {
			sample = append(sample, line...)
		}
	}
	return sample
}

func hasHighByte(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return true
		}
	}
	return false
}

// eastAsianScore scores t, the bytes read as the East Asian encoding name:
// the share of its non-ASCII characters that are frequent in the encoding's
// language (0 when t cannot be text of that language).
func eastAsianScore(name, t string) candidate {
	var letters, kana, hangul, frequent, odd int
	set := frequentChars[name]
	for _, r := range t {
		switch {
		case r < 0x80:
			continue
		case r >= 0x3000 && r <= 0x303F, r >= 0xFF01 && r <= 0xFF60: // CJK and full-width punctuation
			continue
		case r >= 0x80 && r <= 0x9F, r >= 0xE000 && r <= 0xF8FF: // C1 controls, private use
			return candidate{}
		}
		letters++
		switch {
		case r >= 0x3041 && r <= 0x30FF:
			kana++
		case r >= 0xAC00 && r <= 0xD7A3:
			hangul++
		case r >= 0xFF61 && r <= 0xFF9F, r >= 0x3130 && r <= 0x318F: // half-width katakana, isolated jamo
			odd++
		}
		if _, ok := set[r]; ok {
			frequent++
		}
	}
	c := candidate{evidence: letters}
	if letters == 0 {
		return c
	}
	switch name {
	case "shift_jis", "euc-jp":
		if hangul > 0 {
			return c
		}
		frequent += kana
	case "gb18030", "big5":
		if kana > 0 || hangul > 0 {
			return c
		}
	}
	c.score = float64(frequent-odd) / float64(letters)
	return c
}

// westernScore scores t, data read as Windows-1252: the share of the runs of
// high bytes that read as Western text (westernRun) — 0 when there are none
// or a byte is no Windows-1252 character.
func westernScore(data []byte, t string) candidate {
	runes := []rune(t) // Windows-1252 is one character per byte
	if len(runes) != len(data) {
		return candidate{}
	}
	var runs, good int
	for i := 0; i < len(data); {
		if data[i] < 0x80 {
			i++
			continue
		}
		j := i
		for j < len(data) && data[j] >= 0x80 {
			if r := runes[j]; r >= 0x80 && r <= 0x9F {
				return candidate{evidence: runs} // a C1 control: no Windows-1252 text
			}
			j++
		}
		runs++
		if westernRun(data, runes, i, j) {
			good++
		}
		i = j
	}
	if runs == 0 {
		return candidate{}
	}
	return candidate{score: float64(good) / float64(runs), evidence: runs}
}

// westernRun reports whether runes[i:j], the characters of a run of high
// bytes, read as Western text: one character, or two inside a word (a word of
// two accented letters alone is no Western word, but a CJK character read as
// Windows-1252 is two), each a letter of the Western European languages or
// typography, and no typography glued to an ASCII letter but the kind that
// is (quotes, dashes, ¿ ¡ before a word, º ª ° after a number or before C).
func westernRun(data []byte, runes []rune, i, j int) bool {
	before := i > 0 && isASCIILetter(data[i-1])
	after := j < len(data) && isASCIILetter(data[j])
	if j-i > 2 || j-i == 2 && !before && !after {
		return false
	}
	for k := i; k < j; k++ {
		r := runes[k]
		switch {
		case strings.ContainsRune(westernLetters, r):
		case strings.ContainsRune(wordTypography, r):
		case strings.ContainsRune(westernSymbols, r):
			if before || after {
				return false // "§R", "R°": a symbol is no part of a word
			}
		default:
			return false
		}
	}
	return true
}

func isASCIILetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

const (
	// westernLetters are the non-ASCII letters of French, German, Spanish,
	// Portuguese, Italian, Dutch, Danish, Norwegian, Swedish and Finnish in
	// Windows-1252 — not Š, Ž, Ÿ, ƒ (rare there, frequent in East Asian bytes
	// read as Windows-1252) nor Ð, Þ, Ý (Icelandic: Turkish ğ, ş, ı read as
	// Windows-1252).
	westernLetters = "àáâãäåæçèéêëìíîïñòóôõöøùúûüÿœßÀÁÂÃÄÅÆÇÈÉÊËÌÍÎÏÑÒÓÔÕÖØÙÚÛÜŒ"
	// wordTypography sits next to letters: quotes, an apostrophe inside a
	// word (l’école), dashes, ellipsis, marks after a name, the Spanish ¿ ¡
	// before a word, ordinals (nº, 3ª), the degree (25°C) and units (m², m³).
	wordTypography = "‘’‚“”„«»‹›–—…™®¿¡ºª°²³"
	// westernSymbols stand apart from words: currency, signs, fractions.
	westernSymbols = "€†‡‰• ¢£¥§©±´µ¶·¹¼½¾×÷¬¯¨¸¤¦"
)

// frequentChars are, for each East Asian encoding, frequent characters of its
// language beyond kana (common kanji for Japanese; the most frequent
// syllables for Korean; the most frequent characters for Chinese, simplified
// for GB18030, traditional for Big5).
var frequentChars = map[string]map[rune]struct{}{
	"shift_jis": runeSet(jaKanji),
	"euc-jp":    runeSet(jaKanji),
	"euc-kr":    runeSet(koSyllables),
	"gb18030":   runeSet(zhsChars),
	"big5":      runeSet(zhtChars),
}

func runeSet(s string) map[rune]struct{} {
	m := make(map[rune]struct{}, len(s)/3)
	for _, r := range s {
		m[r] = struct{}{}
	}
	return m
}

const jaKanji = "日一国人年大十二本中長出三時行見月分後前生五間上東四今金九入学高円子外八六下来気小七山話女北午百書先名川千水半男西電校語土木聞食車何南万毎白天母火右読友左休父雨" +
	"会社自事者地方物言文法理意定用発業以的部合新問実手動同力化開明関当主心全公味最内作通代度市思家性経現向字漢章設値第機能" +
	"情報報告確認完了終了処理管理番号登録削除変更更新検索表示選択入力設定保存送信受信開始停止状態結果成功失敗内容項目詳細一覧画面取得作成編集追加" +
	"商品顧客注価格数量計額請求支払契約担当営課員議資料予付期対象条件必要可不有無正常異警注重緊急連絡話住所氏齢性別郵便都道府県区町村丁目番地号室様殿御株式限代表取締役" +
	"承認申許却差戻保留止延再完納検収積受荷在庫倉配送運料税込抜消費総小値引割単原利益売損資産負債純" +
	"体制度政治経済産業技術研究開発教育医療保険福祉環境資源農業工業交通通信放送新聞雑誌映画音楽美術写真文化歴史地理科学数学物理化学生物" +
	"思想宗教哲論説明意見感情記憶判断決定選択計画実行評価改善問題解決原因影響関係比較分析調査結論" +
	"春夏秋冬朝昼夜晩今昨明週曜時刻分秒世界海外各都市島港空駅道路橋川湖森林野原畑田家族親兄弟姉妹夫妻息娘孫祖" +
	"言語漢字仮名英語単語文章意味発音読書写話聞" +
	"使持取送届受返借貸買売払答質問頼願望求示指導示教習覚忘知考感思信疑許禁守破押引開閉始終続止進退上昇降落移変換交替" +
	"高低長短広狭深浅重軽強弱早遅速近遠多少新古若老美醜善悪正誤真偽明暗静騒難易簡単複雑特別普通一般全部個別各種" +
	"東京大阪名古屋横浜京都神戸札幌福岡仙台広島北海道沖縄日本中国韓国米英仏独露"

const koSyllables = "이다는의에고하가을로기지서사한어리자대인아도수나시를해구정국일전상라제주들면것장소부위성우니은게생요원문내만적보간비동그마무연할모경신저교등개여되화오실식입있습학과회결공계분세미방물선양관발단중행업체명금차력히와까말했었데점터으운영통출반진안날람본때및또더각며잘알거" +
	"우리그것이것저것사람시간생각정말지금오늘내일어제그리고그러나하지만그래서때문에위해통해대해관련사용사업서비스정보시스템데이터파일설정저장삭제변경확인완료취소검색목록등록수정추가선택입력출력결과오류성공실패시작종료" +
	"한국어문장파일내용인코딩시험합니다입니다있습니다없습니다됩니다했습니다"

const zhsChars = "的一是不了在人有我他这个们中来上大为和国地到以说时要就出会可也你对生能而子那得于着下自之年过发后作里用道行所然家种事成方多经么去法学如都同现当没动面起看定天分还进好小部其些主样理心她本前开但因只从想实日者意无力它与长把机十民第公此已工使情明性知全三又关点正业外将两高间由问很最重并物手应向头文体政美相见被利什二等产或新己制身果加西月话合回特代内信表化老给世位次度门任常先海通教儿原东声提立及比员解水名真论处走义各入几口认条平系气题活更别打女变四总何电数安少报才结反受目太量再感建务做接必场件计管期市直资命山金指许统区保至形社便空决治展科司五基书非则听白界达光放强即像难且权思王象完设式色路记南品住告类求据程北边死张该交规万取格望觉术领共确传师观清今切院让识候带导运" +
	"读取配置文件结果标准输出如果发生错误返回退出码程序用户信息获取设置保存删除修改添加查询列表登录注册密码账号系统数据服务网络服务器客户端版本更新下载上传成功失败编码测试简体中文这是一个"

const zhtChars = "的一是不了在人有我他這個們中來上大為和國地到以說時要就出會可也你對生能而子那得於著下自之年過發後作裡用道行所然家種事成方多經麼去法學如都同現當沒動面起看定天分還進好小部其些主樣理心她本前開但因只從想實日者意無力它與長把機十民第公此已工使情明性知全三又關點正業外將兩高間由問很最重並物手應向頭文體政美相見被利什二等產或新己制身果加西月話合回特代內信表化老給世位次度門任常先海通教兒原東聲提立及比員解水名真論處走義各入幾口認條平系氣題活更別打女變四總何電數安少報才結反受目太量再感建務做接必場件計管期市直資命山金指許統區保至形社便空決治展科司五基書非則聽白界達光放強即像難且權思王象完設式色路記南品住告類求據程北邊死張該交規萬取格望覺術領共確傳師觀清今切院讓識候帶導運" +
	"讀取設定檔案結果標準輸出如果發生錯誤回傳結束碼程式使用者資訊取得儲存刪除修改新增查詢列表登入註冊密碼帳號系統資料服務網路伺服器用戶端版本更新下載上傳成功失敗編碼測試繁體中文這是一個"
