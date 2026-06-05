package backend

import "text/template"

// ComplexModeRustTemplate - extended template for complex mode with CSV, IO, regex support
var ComplexModeRustTemplate = template.Must(template.New("rust_complex").Parse(`
use serde::{Deserialize, Serialize};
use serde_json::Value;
use serde_json::json;
use regex::Regex;
use csv::ReaderBuilder;
use csv::WriterBuilder;
use std::io::Cursor;
use encoding_rs;

#[derive(Serialize, Deserialize, Debug)]
pub struct Input {
{{- range .InputFields}}
    pub {{.Name}}: {{.Type}},
{{- end}}
}

#[derive(Serialize, Deserialize, Debug)]
pub struct Output {
{{- range .OutputFields}}
    pub {{.Name}}: {{.Type}},
{{- end}}
}

{{.Body}}

// ============== CSV Module Helpers ==============

fn csv_reader(input: &str) -> Result<Vec<Vec<String>>, String> {
    let mut reader = ReaderBuilder::new()
        .has_headers(false)
        .from_reader(input.as_bytes());
    let mut rows = Vec::new();
    for result in reader.records() {
        match result {
            Ok(record) => {
                rows.push(record.iter().map(|s| s.to_string()).collect());
            }
            Err(e) => {
                log_message(LogLevel::Error, &format!("CSV reader error: {}", e));
                return Err(format!("CSV parsing error: {}", e));
            },
        }
    }
    log_message(LogLevel::Debug, &format!("CSV reader processed {} rows", rows.len()));
    Ok(rows)
}

// Supported character encodings for CSV parsing
#[derive(Clone, Debug, PartialEq)]
enum Encoding {
    Utf8,
    Latin1,  // ISO-8859-1
    Windows1252,
    Utf16Le,
    Utf16Be,
    Utf32Le,
    Utf32Be,
}

impl Encoding {
    fn from_str(s: &str) -> Result<Self, String> {
        match s.to_lowercase().as_str() {
            "utf-8" | "utf8" => Ok(Encoding::Utf8),
            "latin1" | "iso-8859-1" => Ok(Encoding::Latin1),
            "windows-1252" | "cp1252" => Ok(Encoding::Windows1252),
            "utf-16" | "utf16" => Ok(Encoding::Utf16Le), // Default to little-endian
            "utf-16le" | "utf16le" => Ok(Encoding::Utf16Le),
            "utf-16be" | "utf16be" => Ok(Encoding::Utf16Be),
            "utf-32" | "utf32" => Ok(Encoding::Utf32Le), // Default to little-endian
            "utf-32le" | "utf32le" => Ok(Encoding::Utf32Le),
            "utf-32be" | "utf32be" => Ok(Encoding::Utf32Be),
            _ => Err(format!("Unsupported encoding: {}", s)),
        }
    }

    fn to_string(&self) -> String {
        match self {
            Encoding::Utf8 => "utf-8",
            Encoding::Latin1 => "latin1",
            Encoding::Windows1252 => "windows-1252",
            Encoding::Utf16Le => "utf-16le",
            Encoding::Utf16Be => "utf-16be",
            Encoding::Utf32Le => "utf-32le",
            Encoding::Utf32Be => "utf-32be",
        }.to_string()
    }
}

// Null value detection strategies
#[derive(Clone, Debug, PartialEq)]
enum NullValueStrategy {
    EmptyString,           // Only empty strings are null
    CustomValues(Vec<String>), // Custom list of null value strings
    EmptyAndNa,           // Empty strings and "NA", "N/A", "null", etc.
}

// Type inference modes for automatic type conversion
#[derive(Clone, Debug, PartialEq)]
enum TypeInferenceMode {
    None,                  // No type inference, keep as strings
    Basic,                 // Basic: numbers, booleans, nulls
    Aggressive,            // Try to parse more formats (dates, etc.)
}

// CSV parsing options for enhanced flexibility and edge case handling
#[derive(Clone)]
struct CsvOptions {
    // Basic parsing options
    delimiter: char,
    quote: char,
    escape: Option<char>,
    has_headers: bool,
    flexible: bool,  // Allow inconsistent column counts
    trim_whitespace: bool,
    skip_empty_rows: bool,
    comment: Option<char>,
    encoding: Encoding,

    // Advanced parsing options
    null_strategy: NullValueStrategy,
    type_inference: TypeInferenceMode,
    max_records: Option<usize>,  // Limit number of records to read
    skip_initial_space: bool,    // Skip spaces after delimiter
    double_quote: bool,          // Whether to use double quotes for escaping
    terminator: Option<char>,    // Custom line terminator
    buffer_capacity: usize,      // Buffer size for reading
    allow_trailing_comma: bool,  // Allow trailing commas in records
}

impl Default for CsvOptions {
    fn default() -> Self {
        CsvOptions {
            delimiter: ',',
            quote: '"',
            escape: None,
            has_headers: true,
            flexible: true,  // Allow inconsistent columns by default
            trim_whitespace: false,
            skip_empty_rows: true,
            comment: None,
            encoding: Encoding::Utf8,

            // Advanced defaults
            null_strategy: NullValueStrategy::EmptyAndNa,
            type_inference: TypeInferenceMode::Basic,
            max_records: None,
            skip_initial_space: false,
            double_quote: true,
            terminator: Some('\n'),
            buffer_capacity: 8 * 1024,  // 8KB buffer
            allow_trailing_comma: false,
        }
    }
}

impl CsvOptions {
    fn new() -> Self {
        Self::default()
    }

    fn with_delimiter(mut self, delimiter: char) -> Self {
        self.delimiter = delimiter;
        self
    }

    fn with_quote(mut self, quote: char) -> Self {
        self.quote = quote;
        self
    }

    fn with_escape(mut self, escape: char) -> Self {
        self.escape = Some(escape);
        self
    }

    fn has_headers(mut self, has_headers: bool) -> Self {
        self.has_headers = has_headers;
        self
    }

    fn flexible(mut self, flexible: bool) -> Self {
        self.flexible = flexible;
        self
    }

    fn trim_whitespace(mut self, trim: bool) -> Self {
        self.trim_whitespace = trim;
        self
    }

    fn skip_empty_rows(mut self, skip: bool) -> Self {
        self.skip_empty_rows = skip;
        self
    }

    fn with_comment(mut self, comment: char) -> Self {
        self.comment = Some(comment);
        self
    }

    fn with_encoding(mut self, encoding: Encoding) -> Self {
        self.encoding = encoding;
        self
    }

    fn with_null_strategy(mut self, strategy: NullValueStrategy) -> Self {
        self.null_strategy = strategy;
        self
    }

    fn with_type_inference(mut self, mode: TypeInferenceMode) -> Self {
        self.type_inference = mode;
        self
    }

    fn with_max_records(mut self, max: usize) -> Self {
        self.max_records = Some(max);
        self
    }

    fn skip_initial_space(mut self, skip: bool) -> Self {
        self.skip_initial_space = skip;
        self
    }

    fn double_quote(mut self, double_quote: bool) -> Self {
        self.double_quote = double_quote;
        self
    }

    fn with_terminator(mut self, terminator: char) -> Self {
        self.terminator = Some(terminator);
        self
    }

    fn buffer_capacity(mut self, capacity: usize) -> Self {
        self.buffer_capacity = capacity;
        self
    }

    fn allow_trailing_comma(mut self, allow: bool) -> Self {
        self.allow_trailing_comma = allow;
        self
    }
}

// Encoding conversion utilities using encoding_rs for production-grade support.
fn encoding_rs_label(encoding: &Encoding) -> Option<&'static encoding_rs::Encoding> {
    match encoding {
        Encoding::Utf8 => encoding_rs::Encoding::for_label(b"utf-8"),
        Encoding::Latin1 => encoding_rs::Encoding::for_label(b"iso-8859-1"),
        Encoding::Windows1252 => encoding_rs::Encoding::for_label(b"windows-1252"),
        Encoding::Utf16Le => encoding_rs::Encoding::for_label(b"utf-16le"),
        Encoding::Utf16Be => encoding_rs::Encoding::for_label(b"utf-16be"),
        Encoding::Utf32Le | Encoding::Utf32Be => None, // encoding_rs has no UTF-32; handled below
    }
}

fn decode_utf32_to_utf8(bytes: &[u8], little_endian: bool) -> Result<String, String> {
    if bytes.len() % 4 != 0 {
        return Err("UTF-32 byte length must be a multiple of 4".to_string());
    }
    let mut s = String::new();
    for chunk in bytes.chunks_exact(4) {
        let code_point = if little_endian {
            u32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]])
        } else {
            u32::from_be_bytes([chunk[0], chunk[1], chunk[2], chunk[3]])
        };
        match std::char::from_u32(code_point) {
            Some(c) => s.push(c),
            None => s.push(std::char::REPLACEMENT_CHARACTER),
        }
    }
    Ok(s)
}

fn convert_encoding_to_utf8(bytes: &[u8], encoding: &Encoding) -> Result<String, String> {
    match encoding {
        Encoding::Utf8 => {
            match std::str::from_utf8(bytes) {
                Ok(s) => Ok(s.to_string()),
                Err(e) => Err(format!("Invalid UTF-8 data: {}", e)),
            }
        }
        Encoding::Utf32Le => decode_utf32_to_utf8(bytes, true),
        Encoding::Utf32Be => decode_utf32_to_utf8(bytes, false),
        _ => {
            let enc = encoding_rs_label(encoding).ok_or_else(|| format!("Unsupported encoding: {:?}", encoding))?;
            let (cow, _, _) = enc.decode(bytes);
            Ok(cow.into_owned())
        }
    }
}

fn encode_utf8_to_utf32(s: &str, little_endian: bool) -> Vec<u8> {
    let mut out = Vec::with_capacity(s.chars().count() * 4);
    for c in s.chars() {
        let code = c as u32;
        let bytes = if little_endian { code.to_le_bytes() } else { code.to_be_bytes() };
        out.extend_from_slice(&bytes);
    }
    out
}

// encoding_rs encodes TO the "output encoding" (UTF-8 for UTF-16), so we encode to UTF-16 manually.
fn encode_utf8_to_utf16(s: &str, little_endian: bool) -> Vec<u8> {
    let mut u16s: Vec<u16> = Vec::with_capacity(s.chars().count());
    for c in s.chars() {
        let code = c as u32;
        if code <= 0xFFFF {
            u16s.push(code as u16);
        } else {
            let code = code - 0x1_0000;
            u16s.push(0xD800 | (code >> 10) as u16);
            u16s.push(0xDC00 | (code & 0x3FF) as u16);
        }
    }
    let mut out = Vec::with_capacity(u16s.len() * 2);
    for &u in &u16s {
        let bytes = if little_endian { u.to_le_bytes() } else { u.to_be_bytes() };
        out.extend_from_slice(&bytes);
    }
    out
}

fn convert_utf8_to_encoding(s: &str, encoding: &Encoding) -> Result<Vec<u8>, String> {
    match encoding {
        Encoding::Utf8 => Ok(s.as_bytes().to_vec()),
        Encoding::Utf16Le => Ok(encode_utf8_to_utf16(s, true)),
        Encoding::Utf16Be => Ok(encode_utf8_to_utf16(s, false)),
        Encoding::Utf32Le => Ok(encode_utf8_to_utf32(s, true)),
        Encoding::Utf32Be => Ok(encode_utf8_to_utf32(s, false)),
        _ => {
            let enc = encoding_rs_label(encoding).ok_or_else(|| format!("Unsupported encoding: {:?}", encoding))?;
            let (bytes, _, _) = enc.encode(s);
            Ok(bytes.into_owned())
        }
    }
}

// Null value detection helper
fn is_null_value(value: &str, strategy: &NullValueStrategy) -> bool {
    match strategy {
        NullValueStrategy::EmptyString => value.trim().is_empty(),
        NullValueStrategy::CustomValues(values) => values.contains(&value.to_string()),
        NullValueStrategy::EmptyAndNa => {
            let trimmed = value.trim();
            trimmed.is_empty() ||
            trimmed.eq_ignore_ascii_case("na") ||
            trimmed.eq_ignore_ascii_case("n/a") ||
            trimmed.eq_ignore_ascii_case("null") ||
            trimmed.eq_ignore_ascii_case("none") ||
            trimmed.eq_ignore_ascii_case("#n/a") ||
            trimmed.eq_ignore_ascii_case("#value!") ||
            trimmed.eq_ignore_ascii_case("#ref!")
        }
    }
}

// Advanced type inference with multiple modes
fn infer_value_type(value: &str, mode: &TypeInferenceMode) -> serde_json::Value {
    if value.is_empty() {
        return serde_json::Value::Null;
    }

    match mode {
        TypeInferenceMode::None => serde_json::Value::String(value.to_string()),
        TypeInferenceMode::Basic | TypeInferenceMode::Aggressive => {
            // Try parsing as number
            if let Ok(num) = value.parse::<i64>() {
                return serde_json::Value::Number(serde_json::Number::from(num));
            }
            if let Ok(num) = value.parse::<f64>() {
                if let Some(n) = serde_json::Number::from_f64(num) {
                    return serde_json::Value::Number(n);
                }
            }

            // Try parsing as boolean
            let lower = value.to_lowercase();
            if lower == "true" {
                return serde_json::Value::Bool(true);
            }
            if lower == "false" {
                return serde_json::Value::Bool(false);
            }

            // For aggressive mode, try additional formats
            if let TypeInferenceMode::Aggressive = mode {
                // Could add date parsing, etc. here in the future
            }

            // Default to string
            serde_json::Value::String(value.to_string())
        }
    }
}

// Enhanced CSV reader with comprehensive edge case handling
fn csv_reader_with_options(input: &str, options: CsvOptions) -> Result<(Vec<String>, Vec<std::collections::HashMap<String, serde_json::Value>>), String> {
    // Convert input to UTF-8 if needed
    let utf8_input = if let Encoding::Utf8 = options.encoding {
        input.to_string()
    } else {
        convert_encoding_to_utf8(input.as_bytes(), &options.encoding)?
    };

    let mut reader = ReaderBuilder::new()
        .delimiter(options.delimiter as u8)
        .quote(options.quote as u8)
        .has_headers(options.has_headers)
        .flexible(options.flexible)
        .double_quote(options.double_quote)
        .from_reader(utf8_input.as_bytes());

    // Get headers
    let headers: Vec<String> = if options.has_headers {
        match reader.headers() {
            Ok(h) => h.iter().map(|s| s.to_string()).collect(),
            Err(e) => return Err(format!("CSV headers error: {}", e)),
        }
    } else {
        // Generate column names for headerless CSV
        let mut sample_headers = Vec::new();
        if let Some(Ok(first_record)) = reader.records().next() {
            for i in 0..first_record.len() {
                sample_headers.push(format!("col{}", i + 1));
            }
        }
        sample_headers
    };

    let expected_columns = headers.len();
    let mut records = Vec::new();
    let mut line_number = if options.has_headers { 2 } else { 1 }; // Start counting from data rows
    let mut record_count = 0;

    for result in reader.records() {
        // Check record limit
        if let Some(max) = options.max_records {
            if record_count >= max {
                break;
            }
        }
        match result {
            Ok(record) => {
                // Skip empty rows if configured
                if options.skip_empty_rows && record.iter().all(|s| s.trim().is_empty()) {
                    line_number += 1;
                    continue;
                }

                // Validate column count
                let record_len = record.len();
                if !options.flexible && record_len != expected_columns {
                    return Err(format!(
                        "CSV column count mismatch at line {}: expected {}, got {}. Set flexible=true to allow inconsistent columns.",
                        line_number, expected_columns, record_len
                    ));
                }

                let mut row = std::collections::HashMap::new();
                for (i, header) in headers.iter().enumerate() {
                    let raw_value = record.get(i).unwrap_or("").to_string();
                    let processed_value = if options.trim_whitespace {
                        raw_value.trim().to_string()
                    } else {
                        raw_value
                    };

                    // Use advanced null value detection and type inference
                    let json_value = if is_null_value(&processed_value, &options.null_strategy) {
                        serde_json::Value::Null
                    } else {
                        infer_value_type(&processed_value, &options.type_inference)
                    };

                    row.insert(header.clone(), json_value);
                }
                records.push(row);
                record_count += 1;
            }
            Err(e) => {
                return Err(format!("CSV record error at line {}: {}", line_number, e));
            }
        }
        line_number += 1;
    }

    Ok((headers, records))
}

// Backward compatibility function - uses default options
fn csv_reader_with_headers(input: &str) -> Result<(Vec<String>, Vec<std::collections::HashMap<String, String>>), String> {
    let mut reader = CsvDictReader::new(input)?;
    let headers = reader.headers.clone();
    let mut records = Vec::new();

    for result in reader {
        match result {
            Ok(row) => {
                // Convert serde_json::Value back to String for backward compatibility
                let string_row = row.into_iter().map(|(k, v)| {
                    let str_val = match v {
                        serde_json::Value::String(s) => s,
                        serde_json::Value::Number(n) => n.to_string(),
                        serde_json::Value::Bool(b) => b.to_string(),
                        serde_json::Value::Null => String::new(),
                        _ => v.to_string(),
                    };
                    (k, str_val)
                }).collect();
                records.push(string_row);
            }
            Err(e) => return Err(e),
        }
    }

    Ok((headers, records))
}

fn csv_writer(rows: &Vec<Vec<String>>) -> Result<String, String> {
    let mut wtr = WriterBuilder::new().from_writer(vec![]);
    for row in rows {
        if let Err(e) = wtr.write_record(row) {
            return Err(format!("CSV write error: {}", e));
        }
    }
    match wtr.into_inner() {
        Ok(bytes) => Ok(bytes.into_iter().map(|b| b as char).collect()),
        Err(e) => Err(format!("CSV writer flush error: {}", e)),
    }
}

fn csv_reader_with_delimiter(input: &str, delimiter: char) -> Result<Vec<Vec<String>>, String> {
    let mut reader = ReaderBuilder::new()
        .has_headers(false)
        .delimiter(delimiter as u8)
        .from_reader(input.as_bytes());
    let mut rows = Vec::new();
    for result in reader.records() {
        match result {
            Ok(record) => {
                rows.push(record.iter().map(|s| s.to_string()).collect());
            }
            Err(e) => {
                eprintln!("CSV reader error: {}", e);
                return Err(format!("CSV parsing error: {}", e));
            },
        }
    }
    Ok(rows)
}

// ============== IO Module Helpers ==============

struct StringIO {
    buffer: Vec<u8>,
}

impl StringIO {
    fn new() -> Self {
        StringIO { buffer: Vec::new() }
    }

    fn from_string(s: &str) -> Self {
        StringIO { buffer: s.as_bytes().to_vec() }
    }

    fn write(&mut self, s: &str) {
        self.buffer.extend(s.as_bytes());
    }

    fn read(&self) -> String {
        String::from_utf8_lossy(&self.buffer).into_owned()
    }

    fn getvalue(&self) -> String {
        String::from_utf8_lossy(&self.buffer).into_owned()
    }

    fn clear(&mut self) {
        self.buffer.clear();
        self.buffer.shrink_to_fit(); // Reclaim memory
    }
}

impl Drop for StringIO {
    fn drop(&mut self) {
        self.clear();
        log_message(LogLevel::Debug, "StringIO dropped and cleaned up");
    }
}

struct BytesIO {
    buffer: Vec<u8>,
    position: usize,
}

impl BytesIO {
    fn new() -> Self {
        BytesIO { buffer: Vec::new(), position: 0 }
    }

    fn from_bytes(bytes: &[u8]) -> Self {
        BytesIO { buffer: bytes.to_vec(), position: 0 }
    }

    fn write(&mut self, bytes: &[u8]) {
        self.buffer.extend(bytes);
    }

    fn read(&mut self, len: usize) -> Vec<u8> {
        let end = std::cmp::min(self.position + len, self.buffer.len());
        let result = self.buffer[self.position..end].to_vec();
        self.position = end;
        result
    }

    fn getvalue(&self) -> Vec<u8> {
        self.buffer.clone()
    }

    fn clear(&mut self) {
        self.buffer.clear();
        self.buffer.shrink_to_fit(); // Reclaim memory
        self.position = 0;
    }
}

impl Drop for BytesIO {
    fn drop(&mut self) {
        self.clear();
        log_message(LogLevel::Debug, "BytesIO dropped and cleaned up");
    }
}

// ============== CSV DictReader Helper ==============

// Streaming CSV DictReader for memory-efficient processing of large files
struct CsvDictReader {
    headers: Vec<String>,
    reader: csv::Reader<std::io::Cursor<String>>,
    options: CsvOptions,
    line_number: usize,
    record_count: usize,
}

impl CsvDictReader {
    fn new(input: &str) -> Result<Self, String> {
        Self::new_with_options(input, CsvOptions::default())
    }

    fn new_with_options(input: &str, options: CsvOptions) -> Result<Self, String> {
        // Convert input to UTF-8 if needed
        let utf8_input = if let Encoding::Utf8 = options.encoding {
            input.to_string()
        } else {
            convert_encoding_to_utf8(input.as_bytes(), &options.encoding)?
        };

        let cursor = std::io::Cursor::new(utf8_input);
        let mut reader = csv::ReaderBuilder::new()
            .delimiter(options.delimiter as u8)
            .quote(options.quote as u8)
            .has_headers(options.has_headers)
            .flexible(options.flexible)
            .double_quote(options.double_quote)
            .from_reader(cursor);

        // Get headers
        let headers: Vec<String> = if options.has_headers {
            match reader.headers() {
                Ok(h) => h.iter().map(|s| s.to_string()).collect(),
                Err(e) => return Err(format!("CSV headers error: {}", e)),
            }
        } else {
            // Generate column names for headerless CSV
            let mut sample_headers = Vec::new();
            if let Some(Ok(first_record)) = reader.records().next() {
                for i in 0..first_record.len() {
                    sample_headers.push(format!("col{}", i + 1));
                }
            }
            sample_headers
        };

        Ok(CsvDictReader {
            headers,
            reader,
            options: options.clone(),
            line_number: if options.has_headers { 1 } else { 0 },
            record_count: 0,
        })
    }

    // Constructor that takes a StringIO (for compatibility)
    fn from_stringio(input: StringIO) -> Result<Self, String> {
        let content = input.getvalue();
        Self::new(&content)
    }

    // Auto-detect delimiter and create reader
    fn from_string_with_auto_delimiter(input: &str) -> Result<Self, String> {
        let options = detect_csv_options(input)?;
        Self::new_with_options(input, options)
    }

    // Get column count
    fn column_count(&self) -> usize {
        self.headers.len()
    }
}

impl Iterator for CsvDictReader {
    type Item = Result<std::collections::HashMap<String, serde_json::Value>, String>;

    fn next(&mut self) -> Option<Self::Item> {
        // Check record limit
        if let Some(max) = self.options.max_records {
            if self.record_count >= max {
                return None;
            }
        }

        loop {
            match self.reader.records().next() {
                Some(result) => {
                    self.line_number += 1;
                    match result {
                        Ok(record) => {
                            // Skip empty rows if configured
                            if self.options.skip_empty_rows && record.iter().all(|s| s.trim().is_empty()) {
                                continue;
                            }

                            // Validate column count
                            let record_len = record.len();
                            if !self.options.flexible && record_len != self.headers.len() {
                                return Some(Err(format!(
                                    "CSV column count mismatch at line {}: expected {}, got {}.",
                                    self.line_number, self.headers.len(), record_len
                                )));
                            }

                            let mut row = std::collections::HashMap::new();
                            for (i, header) in self.headers.iter().enumerate() {
                                let raw_value = record.get(i).unwrap_or("").to_string();
                                let processed_value = if self.options.trim_whitespace {
                                    raw_value.trim().to_string()
                                } else {
                                    raw_value
                                };

                                // Use advanced null value detection and type inference
                                let json_value = if is_null_value(&processed_value, &self.options.null_strategy) {
                                    serde_json::Value::Null
                                } else {
                                    infer_value_type(&processed_value, &self.options.type_inference)
                                };

                                row.insert(header.clone(), json_value);
                            }
                            self.record_count += 1;
                            return Some(Ok(row));
                        }
                        Err(e) => {
                            return Some(Err(format!("CSV record error at line {}: {}", self.line_number, e)));
                        }
                    }
                }
                None => return None,
            }
        }
    }
}

// Auto-detect CSV options (delimiter, quote character, etc.)
fn detect_csv_options(input: &str) -> Result<CsvOptions, String> {
    let mut options = CsvOptions::default();

    // Simple heuristic: count potential delimiters in first few lines
    let lines: Vec<&str> = input.lines().take(5).collect();
    if lines.is_empty() {
        return Ok(options);
    }

    let mut comma_count = 0;
    let mut tab_count = 0;
    let mut pipe_count = 0;
    let mut semicolon_count = 0;
    let mut colon_count = 0;

    for line in &lines {
        comma_count += line.chars().filter(|&c| c == ',').count();
        tab_count += line.chars().filter(|&c| c == '\t').count();
        pipe_count += line.chars().filter(|&c| c == '|').count();
        semicolon_count += line.chars().filter(|&c| c == ';').count();
        colon_count += line.chars().filter(|&c| c == ':').count();
    }

    // Choose delimiter with highest count, preferring comma as default
    let max_count = comma_count.max(tab_count).max(pipe_count).max(semicolon_count).max(colon_count);

    if max_count > 0 {
        if tab_count == max_count {
            options.delimiter = '\t';
        } else if pipe_count == max_count {
            options.delimiter = '|';
        } else if semicolon_count == max_count {
            options.delimiter = ';';
        } else if colon_count == max_count {
            options.delimiter = ':';
        } else {
            options.delimiter = ',';
        }
    }

    // Basic header detection: check if first row looks like headers
    // (contains mostly strings, not numbers)
    if lines.len() > 1 {
        let first_line = lines[0];
        let second_line = lines[1];

        // Split by detected delimiter
        let first_fields: Vec<&str> = first_line.split(options.delimiter).collect();
        let second_fields: Vec<&str> = second_line.split(options.delimiter).collect();

        // Simple heuristic: if first row has more non-numeric fields than second row,
        // it's likely headers
        let first_numeric = first_fields.iter().filter(|f| f.parse::<f64>().is_ok()).count();
        let second_numeric = second_fields.iter().filter(|f| f.parse::<f64>().is_ok()).count();

        // If first row has fewer numeric fields than second row, likely has headers
        options.has_headers = first_numeric <= second_numeric;
    }

    Ok(options)
}

// ============== CSV DictWriter Helper ==============

struct CsvDictWriter {
    buffer: StringIO,
    fieldnames: Vec<String>,
    header_written: bool,
}

impl CsvDictWriter {
    fn new(output: StringIO) -> Self {
        CsvDictWriter {
            buffer: output,
            fieldnames: Vec::new(),
            header_written: false,
        }
    }

    fn with_fieldnames(mut self, fieldnames: Vec<String>) -> Self {
        self.fieldnames = fieldnames;
        self
    }

    fn writeheader(&mut self) {
        if !self.header_written {
            let header = self.fieldnames.join(",");
            self.buffer.write(&header);
            self.buffer.write("\n");
            self.header_written = true;
        }
    }

    fn writerow(&mut self, row: &serde_json::Value) -> Result<(), String> {
        if !self.header_written {
            self.writeheader();
        }
        let mut values: Vec<String> = Vec::new();
        for field in &self.fieldnames {
            let val = row.get(field).unwrap_or(&serde_json::Value::Null);
            let value_str = match val {
                serde_json::Value::String(s) => s.clone(),
                serde_json::Value::Number(n) => n.to_string(),
                serde_json::Value::Bool(b) => b.to_string(),
                serde_json::Value::Null => String::new(),
                _ => match serde_json::to_string(val) {
                    Ok(s) => s,
                    Err(e) => return Err(format!("JSON serialization error: {}", e)),
                },
            };
            values.push(value_str);
        }
        self.buffer.write(&values.join(","));
        self.buffer.write("\n");
        Ok(())
    }

    fn writerows(&mut self, rows: &serde_json::Value) -> Result<(), String> {
        if let Some(arr) = rows.as_array() {
            for row in arr {
                self.writerow(row)?;
            }
        }
        Ok(())
    }

    fn getvalue(&self) -> String {
        self.buffer.getvalue()
    }
}

// ============== Regex Module Helpers ==============

fn re_match(pattern: &str, text: &str) -> Option<Vec<String>> {
    let re = Regex::new(pattern).ok()?;
    re.captures(text).map(|caps| {
        caps.iter()
            .filter_map(|c| c.map(|m| m.as_str().to_string()))
            .collect()
    })
}

fn re_search(pattern: &str, text: &str) -> Option<Vec<String>> {
    re_match(pattern, text)
}

fn re_sub(pattern: &str, repl: &str, text: &str) -> Result<String, String> {
    match Regex::new(pattern) {
        Ok(re) => Ok(re.replace_all(text, repl).to_string()),
        Err(e) => Err(format!("Invalid regex pattern: {}", e)),
    }
}

fn re_findall(pattern: &str, text: &str) -> Result<Vec<String>, String> {
    match Regex::new(pattern) {
        Ok(re) => Ok(re.find_iter(text).map(|m| m.as_str().to_string()).collect()),
        Err(e) => Err(format!("Invalid regex pattern: {}", e)),
    }
}

fn re_split(pattern: &str, text: &str) -> Result<Vec<String>, String> {
    match Regex::new(pattern) {
        Ok(re) => Ok(re.split(text).map(|s| s.to_string()).collect()),
        Err(e) => Err(format!("Invalid regex pattern: {}", e)),
    }
}

// ============== Hash Module Helpers ==============

use sha2::{Sha256, Digest};

fn hash_sha256(data: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data.as_bytes());
    format!("{:x}", hasher.finalize())
}

fn hash_md5(data: &str) -> String {
    // MD5 is not recommended for security, but included for compatibility
    log_message(LogLevel::Warn, "MD5 hash used - consider using SHA256 for security");
    let digest = md5::compute(data.as_bytes());
    format!("{:x}", digest)
}

// ============== Base64 Module Helpers ==============

fn base64_encode(data: &str) -> String {
    use base64::{Engine as _, engine::general_purpose};
    general_purpose::STANDARD.encode(data.as_bytes())
}

fn base64_decode(data: &str) -> Result<String, String> {
    use base64::{Engine as _, engine::general_purpose};
    general_purpose::STANDARD.decode(data)
        .map(|bytes| String::from_utf8_lossy(&bytes).to_string())
        .map_err(|e| e.to_string())
}

// ============== JSON Module Helpers ==============

// json_loads handles both string and Value inputs
// If the input is a string, parse it as JSON
// If the input is already a Value, return it as-is
fn json_loads(value: &serde_json::Value) -> serde_json::Value {
    if let Some(s) = value.as_str() {
        // It's a string, try to parse as JSON
        match serde_json::from_str(s) {
            Ok(parsed) => parsed,
            Err(_) => value.clone(), // Return original if parse fails
        }
    } else {
        // Already a Value, return as-is
        value.clone()
    }
}

// json_loads_str parses a string directly as JSON
fn json_loads_str(s: &str) -> serde_json::Value {
    match serde_json::from_str(s) {
        Ok(parsed) => parsed,
        Err(_) => serde_json::Value::String(s.to_string()), // Return as string value if parse fails
    }
}

// ============== Logging Helpers ==============
// Host imports: the FunctionFly WASM host provides
//   functionfly::log(msg_ptr, msg_len)  - no-op (kept for forward compat)
//   functionfly::free(ptr, len)         - release a buffer returned by alloc
#[link(wasm_import_module = "functionfly")]
extern "C" {
    fn log(msg_ptr: *const u8, msg_len: i32);
    fn free(ptr: *mut u8, len: i32);
}

#[derive(Debug)]
enum LogLevel {
    Error,
    Warn,
    Info,
    Debug,
}

fn log_message(level: LogLevel, message: &str) {
    let level_str = match level {
        LogLevel::Error => "ERROR",
        LogLevel::Warn => "WARN",
        LogLevel::Info => "INFO",
        LogLevel::Debug => "DEBUG",
    };
    let line = format!("[{}] {}", level_str, message);
    let bytes = line.as_bytes();
    unsafe {
        log(bytes.as_ptr(), bytes.len() as i32);
    }
}

// ============== WASI Entry Point ==============

#[no_mangle]
pub extern "C" fn handler(input_ptr: i32, input_len: i32) -> i32 {
    let input = parse_input(input_ptr, input_len);
    let result = handler_func(input);
    serialize_output(&result)
}

// WASI _start function - reads input from stdin and writes result to stdout
#[no_mangle]
pub extern "C" fn _start() {
    use std::io::{self, Read, Write};

    // Read input from stdin
    let mut input_buf = String::new();
    if let Err(_) = io::stdin().read_to_string(&mut input_buf) {
        input_buf = "{}".to_string();
    }

    // Parse input - expect JSON with "input" field containing the actual input
    // The input field contains the value to pass to handler as event
    let input = if let Ok(json) = serde_json::from_str::<serde_json::Value>(&input_buf) {
        if let Some(input_val) = json.get("input") {
            // Construct Input struct with event set to the input value
            Input {
{{- range .InputFields}}
                {{.Name}}: input_val.clone(),
{{- end}}
            }
        } else {
            // No "input" field - use entire JSON as event
            Input {
{{- range .InputFields}}
                {{.Name}}: json.clone(),
{{- end}}
            }
        }
    } else {
        // Failed to parse JSON - use default Input
        Input {
{{- range .InputFields}}
            {{.Name}}: {{.DefaultValue}},
{{- end}}
        }
    };

    // Execute handler
    let result = handler_func(input);

    // Serialize and write to stdout
    let output = match serde_json::to_string(&result) {
        Ok(s) => s,
        Err(_) => r#"{"result":""}"#.to_string(),
    };

    // Write to stdout
    let _ = io::stdout().write_all(output.as_bytes());
    let _ = io::stdout().flush();
}

// main function - alternative entry point for some WASM runtimes
#[no_mangle]
pub extern "C" fn main() {
    _start();
}

fn parse_input(ptr: i32, len: i32) -> Input {
    // Reject negative, oversize, or NULL inputs up front. The host
    // already bounds-checks (ptr, len) against the live linear memory
    // size, but defending at the guest boundary as well makes the
    // unsafe block below a one-line trust assumption rather than a
    // pile of implicit checks.
    if len < 0 || len > 1024 * 1024 || ptr <= 0 {
        return Input {
{{- range .InputFields}}
            {{.Name}}: {{.DefaultValue}},
{{- end}}
        };
    }
    // SAFETY: ptr and len were both produced by the host's alloc
    // import and have been bounds-checked against the live linear
    // memory. len is non-negative and bounded by 1 MiB so
    // 'len as usize' cannot wrap on wasm32.
    let slice = unsafe { std::slice::from_raw_parts(ptr as *const u8, len as usize) };
    let json_str = match std::str::from_utf8(slice) {
        Ok(s) => s,
        Err(_) => "{}",
    };
    match serde_json::from_str(json_str) {
        Ok(input) => input,
        Err(_) => Input {
{{- range .InputFields}}
            {{.Name}}: {{.DefaultValue}},
{{- end}}
        },
    }
}

fn serialize_output(output: &Output) -> i32 {
    let json_str = match serde_json::to_string(output) {
        Ok(s) => s,
        Err(_) => "{}".to_string(),
    };
    let bytes = json_str.into_bytes();
    let len = bytes.len();
    // The host caps output at 256 KiB; we cap earlier to be defensive.
    if len > 0xFFFF {
        return 0;
    }
    let len_i32 = len as i32;
    let ptr = bytes.as_ptr() as i32;
    // Transfer ownership of the buffer to the host. The bytes are
    // heap-allocated by the Rust allocator (dlmalloc on wasi); the
    // host's Store is dropped at the end of every call, which
    // reclaims the whole linear memory in one go, so this forget
    // is sound in the current architecture. A future revision can
    // route this through the host's alloc/free ABI to make the
    // lifetime explicit.
    std::mem::forget(bytes);
    (ptr << 16) | (len_i32 & 0xFFFF)
}`))

// DeterministicModeRustTemplate - minimal template for deterministic mode
var DeterministicModeRustTemplate = template.Must(template.New("rust_deterministic").Parse(`
use serde::{Deserialize, Serialize};
use serde_json::Value;
use serde_json::json;

#[derive(Serialize, Deserialize, Debug)]
pub struct Input {
{{- range .InputFields}}
    pub {{.Name}}: {{.Type}},
{{- end}}
}

#[derive(Serialize, Deserialize, Debug)]
pub struct Output {
{{- range .OutputFields}}
    pub {{.Name}}: {{.Type}},
{{- end}}
}

{{.Body}}

#[no_mangle]
pub extern "C" fn handler(input_ptr: i32, input_len: i32) -> i32 {
    let input = parse_input(input_ptr, input_len);
    let result = handler_func(input);
    serialize_output(&result)
}

// WASI _start - entry point for server/sandbox runtime (stdin -> handler_func -> stdout)
#[no_mangle]
pub extern "C" fn _start() {
    use std::io::{self, Read, Write};

    let mut input_buf = String::new();
    if io::stdin().read_to_string(&mut input_buf).is_err() {
        input_buf = "{}".to_string();
    }

    let input = if let Ok(json) = serde_json::from_str::<serde_json::Value>(&input_buf) {
        if let Some(input_val) = json.get("input") {
            Input {
{{- range .InputFields}}
                {{.Name}}: input_val.clone(),
{{- end}}
            }
        } else {
            Input {
{{- range .InputFields}}
                {{.Name}}: json.clone(),
{{- end}}
            }
        }
    } else {
        Input {
{{- range .InputFields}}
            {{.Name}}: {{.DefaultValue}},
{{- end}}
        }
    };

    let result = handler_func(input);
    let output = serde_json::to_string(&result).unwrap_or_else(|_| r#"{"result":""}"#.to_string());
    let _ = io::stdout().write_all(output.as_bytes());
    let _ = io::stdout().flush();
}

// main function - alternative entry point for some WASM runtimes
#[no_mangle]
pub extern "C" fn main() {
    _start();
}

fn parse_input(ptr: i32, len: i32) -> Input {
    // Reject negative, oversize, or NULL inputs up front. The host
    // already bounds-checks (ptr, len) against the live linear memory
    // size, but defending at the guest boundary as well makes the
    // unsafe block below a one-line trust assumption rather than a
    // pile of implicit checks.
    if len < 0 || len > 1024 * 1024 || ptr <= 0 {
        return Input {
{{- range .InputFields}}
            {{.Name}}: {{.DefaultValue}},
{{- end}}
        };
    }
    // SAFETY: ptr and len were both produced by the host's alloc
    // import and have been bounds-checked against the live linear
    // memory. len is non-negative and bounded by 1 MiB so
    // 'len as usize' cannot wrap on wasm32.
    let slice = unsafe { std::slice::from_raw_parts(ptr as *const u8, len as usize) };
    let json_str = match std::str::from_utf8(slice) {
        Ok(s) => s,
        Err(_) => "{}",
    };
    match serde_json::from_str(json_str) {
        Ok(input) => input,
        Err(_) => Input {
{{- range .InputFields}}
            {{.Name}}: {{.DefaultValue}},
{{- end}}
        },
    }
}

fn serialize_output(output: &Output) -> i32 {
    let json_str = match serde_json::to_string(output) {
        Ok(s) => s,
        Err(_) => "{}".to_string(),
    };
    let bytes = json_str.into_bytes();
    let len = bytes.len();
    // The host caps output at 256 KiB; we cap earlier to be defensive.
    if len > 0xFFFF {
        return 0;
    }
    let len_i32 = len as i32;
    let ptr = bytes.as_ptr() as i32;
    // Transfer ownership of the buffer to the host. The bytes are
    // heap-allocated by the Rust allocator (dlmalloc on wasi); the
    // host's Store is dropped at the end of every call, which
    // reclaims the whole linear memory in one go, so this forget
    // is sound in the current architecture. A future revision can
    // route this through the host's alloc/free ABI to make the
    // lifetime explicit.
    std::mem::forget(bytes);
    (ptr << 16) | (len_i32 & 0xFFFF)
}

// json_loads handles both string and Value inputs
fn json_loads(value: &serde_json::Value) -> serde_json::Value {
    if let Some(s) = value.as_str() {
        match serde_json::from_str(s) {
            Ok(parsed) => parsed,
            Err(_) => value.clone(),
        }
    } else {
        value.clone()
    }
}

// json_loads_str parses a string directly as JSON
fn json_loads_str(s: &str) -> serde_json::Value {
    match serde_json::from_str(s) {
        Ok(parsed) => parsed,
        Err(_) => serde_json::Value::String(s.to_string()),
    }
}`))
