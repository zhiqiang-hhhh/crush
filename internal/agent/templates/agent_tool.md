Launch a new agent that has access to the following tools: GlobTool, GrepTool, LS, View. When you are searching for a keyword or file and are not confident that you will find the right match on the first try, use the Agent tool to perform the search for you.

<when_to_use>
- Searching for a keyword like "config" or "logger", or questions like "which file does X?"
- Open-ended research that may produce excessive output you don't want in your main context
- Independent queries that can be parallelized (launch multiple agents in one message)
- If you already know the specific file path, use View or GlobTool directly instead — faster
- If searching for a specific class definition like "class Foo", use GlobTool directly instead
</when_to_use>

<when_not_to_use>
- NEVER use Agent for tasks achievable in 1-2 direct tool calls (e.g., a single grep or viewing a known file)
- When your context is clean and not overloaded — just call Grep/Glob/View yourself
- When you only need to check one file or one pattern — the overhead of a sub-agent is pure waste
- When you already have a clear idea of where to look — go directly, don't delegate
</when_not_to_use>

<usage_notes>
1. Launch multiple agents concurrently whenever possible, to maximize performance; to do that, use a single message with multiple tool uses
2. When the agent is done, it will return a single message back to you. The result returned by the agent is not visible to the user. To show the user the result, you should send a text message back to the user with a concise summary of the result.
3. Each agent invocation is stateless. You will not be able to send additional messages to the agent, nor will the agent be able to communicate with you outside of its final report. Therefore, your prompt should contain a highly detailed task description for the agent to perform autonomously and you should specify exactly what information the agent should return back to you in its final and only message to you.
4. The agent's outputs should generally be trusted
5. IMPORTANT: The agent can not use Bash, Replace, Edit, so can not modify files. If you want to use these tools, use them directly instead of going through the agent.
6. Clearly tell the agent whether you expect it to search for code, find patterns, or analyze architecture — since it has no awareness of your intent.
7. Avoid duplicating work that agents are already doing — if you delegate research to an agent, don't also perform the same searches yourself.
</usage_notes>

<writing_agent_prompts>
The agent starts fresh — it has zero context about your conversation. Write prompts accordingly:
- Brief it like a smart colleague who just walked into the room
- Explain what you're trying to accomplish and why
- Describe what you've already learned or ruled out
- Give enough context that the agent can make judgment calls, not just follow narrow instructions
- For lookups: hand over the exact command or pattern. For investigations: hand over the question
- Never delegate understanding — don't write "based on your findings, fix the bug". Include file paths, line numbers, what specifically to look for
- Terse, command-style prompts produce shallow, generic work
- Specify exactly what information the agent should return in its report
</writing_agent_prompts>
