# Technical challenge documentation by Imanol Rojas
## First steps
- I modified the Makefile such that the contents of the environment file is exported at runtime. This automates the projet deployment making it scalable to new tokens and environment variables.
- I moved Acai documentation to the `doc`folder so the reviewer sees my documentation first. The original documentation can be found [here](/doc/README.md)
- Finally I installed go, and executed the application to check that the repo works as expected.

## Task 1 – Fix conversation title
### Problem

The original Title implementation attempted to use OpenAI to generate a conversation title, but the way the prompt was built caused incorrect behavior:

```go
msgs := make([]openai.ChatCompletionMessageParamUnion, len(conv.Messages))

msgs[0] = openai.AssistantMessage("Generate a concise, descriptive title...")
for i, m := range conv.Messages {
    msgs[i] = openai.UserMessage(m.Content)
}
```

Issues:

- The instruction message set at msgs[0] is overwritten in the for loop.
- All conversation messages are sent as user messages, without a proper system role and system message.
- The model receives the full conversation without explicit rules, so it behaves as a regular assistant and answers the question instead of generating a title.


### Solution

Key design decisions:

- Use the first user message as the source for the title.
- Send a dedicated system message that explicitly defines the expected output.
- Limit the response to a short noun phrase, not an answer.
- Add a safe fallback title if the model response is empty or an error occurs.


The Title method was updated to:

- Extract the first non-empty user message from the conversation.
- Build a minimal prompt with:
    - A system message describing how to format the title.
    - A user message containing only the first user message.
- Call OpenAI with this controlled prompt.
- Fallback to "New conversation" when the response is invalid.
- Post-process the result (trim spaces, remove quotes/newlines, enforce a max length).
- Fallback to "New conversation" when the response after the post-process is empty. invalid.

Snippet of the updated logic:

```go
// 1. Take the first meaningful user message as input.
var firstUserMessage string
for _, m := range conv.Messages {
    if m.Role == model.RoleUser && strings.TrimSpace(m.Content) != "" {
        firstUserMessage = m.Content
        break
    }
}
if firstUserMessage == "" {
    firstUserMessage = conv.Messages[0].Content
}

// 2. Instruct the model explicitly via system message and set the user message
system := openai.SystemMessage(`You generate concise conversation titles.

Rules:
- Output ONLY a short noun phrase summarizing the user's first message.
- Do NOT answer the question.
- Do NOT include quotes.
- Maximum 6 words.`)

user := openai.UserMessage(firstUserMessage)

// 3. Ask OpenAI using [system, user] messages only.
resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
    Model:    openai.ChatModelGPT4_1,
    Messages: []openai.ChatCompletionMessageParamUnion{system, user},
})

// 4. Clean and validate the returned title; fallback if needed.
if err != nil || len(resp.Choices) == 0 {
    return "New conversation", nil
}

title := resp.Choices[0].Message.Content
title = strings.ReplaceAll(title, "\n", " ")
title = strings.Trim(title, " \t\r\n-\"'")

if title == "" {
    return "New conversation", nil
}

if len(title) > 80 {
    title = title[:80]
}
```

### Result

Conversation titles after the fix:

```text
ID                         TITLE
69138ad828032f0e80507808   Advanced LLMs for Programming
69138a7928032f0e80507805   Go programming language syntax overview
69137c4c28032f0e80507802   Barcelona current weather information
```

## Task 1 (Bonus) – Optimize `StartConversation()` performance

## Task 2

## Task 3


## Task 4


## Task 5

