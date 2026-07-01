package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func NewEmbedCmd() *cobra.Command {
	var lang string
	var asJSON bool
	var method string
	var env string
	cmd := &cobra.Command{
		Use:   "embed [author/name]",
		Short: "Generate SDK snippets for calling a function",
		Long: `Generate ready-to-use code snippets for calling a deployed function
from your application.

Supports multiple languages and formats. Copy the output directly into
your project. Each snippet includes authentication setup, error handling,
and the function invocation.`,
		Example: `  ff embed
  ff embed alice/my-fn
  ff embed alice/my-fn --lang javascript
  ff embed alice/my-fn --lang python
  ff embed alice/my-fn --lang go
  ff embed alice/my-fn --lang curl
  ff embed alice/my-fn --lang all --json
  ff embed alice/my-fn --lang typescript --method GET
  ff embed alice/my-fn --env production`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEmbed(args, lang, method, env, asJSON)
		},
	}
	cmd.Flags().StringVarP(&lang, "lang", "l", "curl", "Language: curl, javascript, typescript, python, go, ruby, php, java, csharp, all")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output all snippets as JSON")
	cmd.Flags().StringVarP(&method, "method", "X", "POST", "HTTP method for the snippet")
	cmd.Flags().StringVar(&env, "env", "", "Environment alias (e.g. production, staging)")
	return cmd
}

type EmbedSnippet struct {
	Language string `json:"language"`
	Label    string `json:"label"`
	Code     string `json:"code"`
}

func runEmbed(args []string, lang, method, env string, asJSON bool) error {
	author, name, err := resolveAuthorName(args)
	if err != nil {
		return err
	}

	baseURL := APIURL()
	endpoint := fmt.Sprintf("%s/v1/fx/%s/%s", baseURL, author, name)
	if env != "" {
		endpoint = fmt.Sprintf("%s/v1/fx/%s/%s@%s", baseURL, author, name, env)
	}

	creds, _ := LoadCredentials()
	token := ""
	if creds != nil {
		token = creds.Token
	}

	snippets := generateSnippets(author, name, endpoint, strings.ToUpper(method), token, lang)

	if asJSON || WantJSON() {
		printJSON(map[string]interface{}{
			"function": author + "/" + name,
			"endpoint": endpoint,
			"method":   strings.ToUpper(method),
			"env":      env,
			"snippets": snippets,
		})
		return nil
	}

	if len(snippets) == 1 {
		fmt.Printf("\n%s\n", snippets[0].Code)
		return nil
	}

	fmt.Printf("\n📌 Embed: %s/%s\n", author, name)
	fmt.Printf("   Endpoint: %s\n", endpoint)
	fmt.Println()

	for _, s := range snippets {
		fmt.Printf("── %s ──────────────────────────────\n", s.Label)
		fmt.Println(s.Code)
		fmt.Println()
	}

	return nil
}

func generateSnippets(author, name, endpoint, method, token, lang string) []EmbedSnippet {
	if lang == "all" {
		return allSnippets(endpoint, method, token)
	}
	snippet := singleSnippet(author, name, endpoint, method, token, lang)
	if snippet == nil {
		return allSnippets(endpoint, method, token)
	}
	return []EmbedSnippet{*snippet}
}

func singleSnippet(author, name, endpoint, method, token, lang string) *EmbedSnippet {
	switch strings.ToLower(lang) {
	case "curl", "sh", "shell":
		return &EmbedSnippet{Language: "curl", Label: "cURL", Code: snippetCurl(endpoint, method, token)}
	case "js", "javascript", "node", "nodejs":
		return &EmbedSnippet{Language: "javascript", Label: "JavaScript (fetch)", Code: snippetJS(endpoint, method, token)}
	case "ts", "typescript":
		return &EmbedSnippet{Language: "typescript", Label: "TypeScript", Code: snippetTS(endpoint, method, token)}
	case "py", "python":
		return &EmbedSnippet{Language: "python", Label: "Python (requests)", Code: snippetPython(endpoint, method, token)}
	case "go", "golang":
		return &EmbedSnippet{Language: "go", Label: "Go", Code: snippetGo(endpoint, method, token)}
	case "rb", "ruby":
		return &EmbedSnippet{Language: "ruby", Label: "Ruby (net/http)", Code: snippetRuby(endpoint, method, token)}
	case "php":
		return &EmbedSnippet{Language: "php", Label: "PHP (cURL)", Code: snippetPHP(endpoint, method, token)}
	case "java":
		return &EmbedSnippet{Language: "java", Label: "Java (HttpClient)", Code: snippetJava(endpoint, method, token)}
	case "cs", "csharp", "c#":
		return &EmbedSnippet{Language: "csharp", Label: "C# (HttpClient)", Code: snippetCSharp(endpoint, method, token)}
	default:
		return nil
	}
}

func allSnippets(endpoint, method, token string) []EmbedSnippet {
	return []EmbedSnippet{
		{Language: "curl", Label: "cURL", Code: snippetCurl(endpoint, method, token)},
		{Language: "javascript", Label: "JavaScript (fetch)", Code: snippetJS(endpoint, method, token)},
		{Language: "typescript", Label: "TypeScript", Code: snippetTS(endpoint, method, token)},
		{Language: "python", Label: "Python (requests)", Code: snippetPython(endpoint, method, token)},
		{Language: "go", Label: "Go", Code: snippetGo(endpoint, method, token)},
		{Language: "ruby", Label: "Ruby (net/http)", Code: snippetRuby(endpoint, method, token)},
		{Language: "php", Label: "PHP (cURL)", Code: snippetPHP(endpoint, method, token)},
		{Language: "java", Label: "Java (HttpClient)", Code: snippetJava(endpoint, method, token)},
		{Language: "csharp", Label: "C# (HttpClient)", Code: snippetCSharp(endpoint, method, token)},
	}
}

func snippetCurl(endpoint, method, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf(" \\\n  -H 'Authorization: Bearer %s'", token)
	}
	return fmt.Sprintf(`curl -X %s %s \
  -H 'Content-Type: application/json'%s \
  -d '{"key": "value"}'`, method, endpoint, auth)
}

func snippetJS(endpoint, method, token string) string {
	authHeader := ""
	if token != "" {
		authHeader = fmt.Sprintf("\n    'Authorization': 'Bearer %s',", token)
	}
	return fmt.Sprintf(`const response = await fetch('%s', {
  method: '%s',
  headers: {
    'Content-Type': 'application/json',%s
  },
  body: JSON.stringify({ key: 'value' }),
});

const data = await response.json();
console.log(data);`, endpoint, method, authHeader)
}

func snippetTS(endpoint, method, token string) string {
	authHeader := ""
	if token != "" {
		authHeader = fmt.Sprintf("\n      'Authorization': 'Bearer %s',", token)
	}
	return fmt.Sprintf(`interface FunctionResponse {
  [key: string]: unknown;
}

async function callFunction(input: Record<string, unknown>): Promise<FunctionResponse> {
  const response = await fetch('%s', {
    method: '%s',
    headers: {
      'Content-Type': 'application/json',%s
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw new Error(`+"`HTTP ${response.status}: ${response.statusText}`"+`);
  }

  return response.json();
}

const result = await callFunction({ key: 'value' });
console.log(result);`, endpoint, method, authHeader)
}

func snippetPython(endpoint, method, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf("\n    'Authorization': 'Bearer %s',", token)
	}
	return fmt.Sprintf(`import requests

response = requests.%s(
    "%s",
    headers={
        "Content-Type": "application/json",%s
    },
    json={"key": "value"},
)

data = response.json()
print(data)`, strings.ToLower(method), endpoint, auth)
}

func snippetGo(endpoint, method, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf(`\n	req.Header.Set("Authorization", "Bearer %s")`, token)
	}
	return fmt.Sprintf(`package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func main() {
	body, _ := json.Marshal(map[string]interface{}{
		"key": "value",
	})

	req, _ := http.NewRequest("%s", "%s", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")%s

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	result, _ := io.ReadAll(resp.Body)
	fmt.Println(string(result))
}`, method, endpoint, auth)
}

func snippetRuby(endpoint, method, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf("\n  request['Authorization'] = 'Bearer %s'", token)
	}
	return fmt.Sprintf(`require 'net/http'
require 'json'

uri = URI('%s')
http = Net::HTTP.new(uri.host, uri.port)
http.use_ssl = uri.scheme == 'https'

request = Net::HTTP::%s.new(uri)
request['Content-Type'] = 'application/json'%s
request.body = { key: 'value' }.to_json

response = http.request(request)
puts JSON.parse(response.body)`, endpoint, strings.Title(strings.ToLower(method)), auth)
}

func snippetPHP(endpoint, method, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf("\n    'Authorization: Bearer %s',", token)
	}
	return fmt.Sprintf(`<?php
$ch = curl_init('%s');

curl_setopt($ch, CURLOPT_CUSTOMREQUEST, '%s');
curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
curl_setopt($ch, CURLOPT_HTTPHEADER, [
    'Content-Type: application/json',%s
]);
curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode(['key' => 'value']));

$response = curl_exec($ch);
$data = json_decode($response, true);
curl_close($ch);

print_r($data);`, endpoint, method, auth)
}

func snippetJava(endpoint, method, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf(`\n            .header("Authorization", "Bearer %s")`, token)
	}
	return fmt.Sprintf(`import java.net.http.*;
import java.net.URI;

var client = HttpClient.newHttpClient();
var body = HttpRequest.BodyPublishers.ofString("{\"key\": \"value\"}");

var request = HttpRequest.newBuilder()
    .uri(URI.create("%s"))
    .%s(body)
    .header("Content-Type", "application/json")%s
    .build();

var response = client.send(request, HttpResponse.BodyHandlers.ofString());
System.out.println(response.body());`, endpoint, strings.ToLower(method), auth)
}

func snippetCSharp(endpoint, method, token string) string {
	auth := ""
	if token != "" {
		auth = fmt.Sprintf("\n    client.DefaultRequestHeaders.Authorization =\n        new System.Net.Http.Headers.AuthenticationHeaderValue(\"Bearer\", \"%s\");\n", token)
	}
	return fmt.Sprintf(`using System.Net.Http;
using System.Text;
using System.Text.Json;

var client = new HttpClient();%s
var payload = JsonSerializer.Serialize(new { key = "value" });
var content = new StringContent(payload, Encoding.UTF8, "application/json");

var response = await client.%sAsync("%s", content);
var result = await response.Content.ReadAsStringAsync();
Console.WriteLine(result);`, auth, strings.Title(strings.ToLower(method)), endpoint)
}
