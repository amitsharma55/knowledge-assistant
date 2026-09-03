package bedrock

// Real Bedrock implementation.
//
// Implementation notes (left as scaffolding; wire up when AWS SDK is added
// to go.mod):
//
//   import (
//     "github.com/aws/aws-sdk-go-v2/config"
//     "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
//   )
//
// LLM.Stream should call InvokeModelWithResponseStream with the Anthropic
// messages payload:
//
//   {
//     "anthropic_version": "bedrock-2023-05-31",
//     "max_tokens": 1024,
//     "system": prompt.System,
//     "messages": [{"role":"user","content": prompt.User}]
//   }
//
// Keep the instructions in "system" and only the context and question in the
// user turn -- concatenating them lets the model quote the rules back as if
// they were retrieved content.
//
// Then iterate the event stream, decoding chunks of type
// "content_block_delta" and forwarding delta.text as StreamEvent{Type:"token"}.
//
// Embedder.Embed should call InvokeModel with amazon.titan-embed-text-v2:0
// and payload {"inputText": text, "dimensions": 1024, "normalize": true}
// and return the "embedding" field.
//
// We keep this file as a placeholder so main.go can compile-time switch on
// LLMMode without pulling AWS deps until the team is ready.
