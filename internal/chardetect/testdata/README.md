# Test Data for chardetect

This directory contains test files in various character encodings used to test the `chardetect` package.

## Files

### Basic Encoding Tests

- `utf8.txt` - UTF-8 encoded Japanese text
- `utf8_bom.txt` - UTF-8 with BOM
- `shift-jis.txt` - Shift-JIS encoded Japanese text
- `euc-jp.txt` - EUC-JP encoded Japanese text
- `iso-2022-jp.txt` - ISO-2022-JP encoded Japanese text
- `ascii.txt` - Pure ASCII text

### Complex Tests

- `mixed_sjis_ascii.txt` - Shift-JIS with ASCII mixed
- `kanji_heavy.sjis` - Kanji-heavy Shift-JIS text
- `kanji_heavy.eucjp` - Kanji-heavy EUC-JP text
- `hiragana_only.sjis` - Hiragana-only Shift-JIS
- `katakana_only.eucjp` - Katakana-only EUC-JP

### Edge Cases

- `empty.txt` - Empty file
- `short.txt` - Very short text (< 10 bytes)
- `long_utf8.txt` - Long UTF-8 text (> 10KB)

## Test Content

The Japanese text used in tests is from classic literature and common phrases:

- 「吾輩は猫である。名前はまだ無い。」(夏目漱石)
- 「こんにちは、世界！」(Hello, World!)
- Technical terms and symbols

## Generation

These files are generated programmatically during tests to ensure correct encoding.
See `testdata_generator_test.go` for the generation logic.
