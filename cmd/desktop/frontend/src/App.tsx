import { useEffect, useMemo, useState } from "react";

type SessionInfo = {
  conversationId: string;
};

type MentorMessage = {
  id: string;
  content: string;
  createdAt: string;
};

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          GetSessionInfo?: () => Promise<SessionInfo>;
          GetMessages?: () => Promise<MentorMessage[]>;
        };
      };
    };
  }
}

// make it easier to extract the commands practiced and the most relevant/recent questions asked

export function extractCommands(text: string): string[] {
  const found: string[] = [];
  for (const block of text.matchAll(/```[a-z]*\n([\s\S]*?)```/g)) {
    for (const line of block[1].split("\n")) {
      const cleaned = line.trim().replace(/^\$\s*/, "");
      if (cleaned && !cleaned.startsWith("#")) found.push(cleaned);
    }
  }
  for (const inline of text.matchAll(/`([^`\n]+)`/g)) {
    const cleaned = inline[1].trim();
    if (/^[a-z][\w.-]*( |$)/i.test(cleaned) && cleaned.length <= 120) found.push(cleaned);
  }
  return found;
}

export function extractQuestions(text: string): string[] {
  return (text.match(/[^.!?\n]+\?/g) ?? []).map((q) => q.trim());
}

function App() {
  const [conversationId, setConversationId] = useState("");
  const [messages, setMessages] = useState<MentorMessage[]>([]);

  useEffect(() => {
    let active = true;
    const app = window.go?.main?.App;

    const refresh = async () => {
      try {
        const [sessionInfo, latestMessages] = await Promise.all([
          app?.GetSessionInfo?.(),
          app?.GetMessages?.(),
        ]);
        if (!active) return;
        if (sessionInfo) setConversationId(sessionInfo.conversationId);
        if (latestMessages) {
          setMessages((prev) =>
            prev.length === latestMessages.length &&
            prev[prev.length - 1]?.id === latestMessages[latestMessages.length - 1]?.id
              ? prev
              : latestMessages,
          );
        }
      } catch (error) {
        console.error("could not load the mentor session", error);
      }
    };

    void refresh();
    const interval = window.setInterval(() => void refresh(), 750);
    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, []);
  const commands = useMemo(
  () => [...new Set(messages.flatMap((m) => extractCommands(m.content)))],
  [messages],
  );
  const questions = useMemo(
    () => messages.flatMap((m) => extractQuestions(m.content)).slice(-5),
    [messages],
  );
  const latest = messages[messages.length - 1];

  return (
    <main className="app-shell">
      <aside className="sidebar" aria-label="Application sidebar">
        <div className="brand">
          <div className="brand-mark" aria-hidden="true">
            <TerminalIcon />
          </div>
          <span>Linux Mentor</span>
        </div>

        <div className="session-card">
          <span className="session-label">CURRENT SESSION</span>
          <span className="session-name">Linux learning session</span>
          <span className="session-state">
            <span className="state-dot" />
            {conversationId ? "Connected to Antigravity" : "Waiting for Antigravity"}
          </span>
        </div>
                <div className="companion">
          <h2>Commands to try</h2>
          {commands.length === 0 ? (
            <p className="muted">None yet.</p>
          ) : (
            <ul>
              {commands.map((c) => (
                <li key={c}>
                  <code>{c}</code>
                  <button onClick={() => navigator.clipboard?.writeText(c)}>Copy</button>
                </li>
              ))}
            </ul>
          )}
          <h2>Think about</h2>
          {questions.length === 0 ? (
            <p className="muted">No questions yet.</p>
          ) : (
            <ul>
              {questions.map((q) => (
                <li key={q}>{q}</li>
              ))}
            </ul>
          )}
        </div>

        <div className="sidebar-note">
          <span className="note-icon" aria-hidden="true">
            <LightbulbIcon />
          </span>
          <p>Learn through inquisition. Your mentor will give hints and questions, not do the work for you.</p>
        </div>

        <div className="sidebar-footer">
          <span className="linux-indicator" />
          <span>Linux CLI Mentor</span>
        </div>
      </aside>

      <section className="conversation" aria-label="Mentor responses">
        <header className="topbar">
          <div>
            <p className="eyebrow">YOUR LEARNING SPACE</p>
            <h1>Mentor chat</h1>
          </div>
          <div className="connection-pill">
            <span className="state-dot" />
            {conversationId ? "Session linked" : "Starting session"}
          </div>
        </header>

        <div className={`message-area${messages.length ? " has-messages" : ""}`}>
          {messages.length === 0 ? (
            <div className="welcome">
              <div className="welcome-mark" aria-hidden="true">
                <TerminalIcon />
              </div>
              <p className="eyebrow">THINK IT THROUGH</p>
              <h2>A mentor to help you learn faster and more efficiently.</h2>
              <p className="welcome-copy">
                Your mentor’s explanations and questions will appear here. Keep using
                Antigravity in the terminal to continue your conversation.
              </p>
              <div className="principle">
                <span className="principle-line" />
                <span>Understand the why and how.</span>
              </div>
            </div>
          ) : (
            <div className="message-list" aria-live="polite">
              {(latest ? [latest] : []).map((message) => (
                <article className="mentor-message" key={message.id}>
                  <div className="message-avatar" aria-hidden="true">
                    <TerminalIcon />
                  </div>
                  <div className="message-content">
                    <span className="message-author">Linux CLI Mentor</span>
                    <p>{message.content}</p>
                  </div>
                </article>
              ))}
            </div>
          )}
        </div>

        <footer className="conversation-footer">
          <span className="footer-orbit" aria-hidden="true">
            <SparkleIcon />
          </span>
          <span>
            {messages.length
              ? "Keep exploring in your terminal. Your mentor’s replies will appear here."
              : "Responses from your Antigravity session will show here."}
          </span>
        </footer>
      </section>
    </main>
  );
}

function TerminalIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="m5 7 4 5-4 5" />
      <path d="M12 17h7" />
    </svg>
  );
}

function LightbulbIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M9 18h6M10 21h4m-2-19a7 7 0 0 0-4 12.75c.55.37 1 1.08 1 1.75h6c0-.67.45-1.38 1-1.75A7 7 0 0 0 12 2Z" />
    </svg>
  );
}

function SparkleIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="m12 3 1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8L12 3Z" />
      <path d="m19 16 .8 2.2L22 19l-2.2.8L19 22l-.8-2.2L16 19l2.2-.8L19 16Z" />
    </svg>
  );
}

export default App;
