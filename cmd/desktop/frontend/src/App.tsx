import { useEffect, useMemo, useState } from "react";

type SessionInfo = {
  conversationId: string;
};

type MentorMessage = {
  id: string;
  content: string;
  createdAt: string;
};

type ReviewProgressItem = {
  concept: string;
  status: string; // learning, review, reinforce, or mastered
  due: boolean;
  stage: number; // clean-review steps toward mastery, 0 to 3
  attempts: number;
  lastMisses: number;
  currentMisses: number;
  lastReviewed: string;
  nextReview: string;
  commandAccuracy?: string;
  explanationDepth?: string;
  notes?: string;
};

type LearningProgress = {
  dueForReview: ReviewProgressItem[];
  mastered: ReviewProgressItem[];
  areasToWorkOn: ReviewProgressItem[];
  scheduled: ReviewProgressItem[];
  allItems: ReviewProgressItem[];
};

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          GetSessionInfo?: () => Promise<SessionInfo>;
          GetMessages?: () => Promise<MentorMessage[]>;
          GetLearningProgress?: () => Promise<LearningProgress>;
        };
      };
    };
  }
}

export function extractCommands(text: string): string[] {
  const found: string[] = [];
  // Only extract commands explicitly tagged as verified correct:
  for (const match of text.matchAll(/<!--\s*study:correct-command\s+command="([^"]+)"/g)) {
    const cmd = match[1].trim();
    if (cmd && !found.includes(cmd)) found.push(cmd);
  }
  for (const match of text.matchAll(/<!--\s*study:correct-command:\s*([^->]+?)\s*-->/g)) {
    const cmd = match[1].trim();
    if (cmd && !found.includes(cmd)) found.push(cmd);
  }
  return found;
}

const statusLabels: Record<string, string> = {
  learning: "Learning",
  review: "Needs review",
  reinforce: "Needs work",
  mastered: "Mastered",
};

// One plain-language line about where a concept stands. Mastery takes three clean
// reviews (no wrong answers first) on separate days: 14, 28, then 56 days apart,
// sooner after wrong answers. Correct answers before the review date are practice.
export function describeProgress(item: ReviewProgressItem): string {
  const label = statusLabels[item.status] ?? item.status;
  if (item.status === "mastered") return `${label} · 3 of 3 clean reviews`;
  if (item.status === "reinforce" && item.currentMisses > 0) {
    return `${label} · answer it correctly to schedule the next review`;
  }
  const when = item.due ? "review due now" : `next review ${item.nextReview}`;
  // only the clean track counts toward mastery; after wrong answers it starts over
  const clean = item.status === "learning" ? item.stage : 0;
  return `${label} · ${clean} of 3 clean reviews · ${when}`;
}

export function describeAnswers(item: ReviewProgressItem): string {
  const answers = `${item.attempts} ${item.attempts === 1 ? "answer" : "answers"}`;
  if (item.currentMisses > 0) {
    return `${answers} · ${item.currentMisses} wrong since the last correct answer`;
  }
  if (item.lastMisses > 0) {
    return `${answers} · last review was correct after ${item.lastMisses === 2 ? "2 or more" : "1"} wrong`;
  }
  return answers;
}

export function cleanContent(text: string): string {
  return text.replace(/<!--\s*study:[^>]*-->/g, "").trim();
}

export function extractQuestions(text: string): string[] {
  return (text.match(/[^.!?\n]+\?/g) ?? []).map((q) => q.trim());
}

function App() {
  const [conversationId, setConversationId] = useState("");
  const [messages, setMessages] = useState<MentorMessage[]>([]);
  const [activeTab, setActiveTab] = useState<"guide" | "chat">("guide");
  const [progress, setProgress] = useState<LearningProgress>({
    dueForReview: [],
    mastered: [],
    areasToWorkOn: [],
    scheduled: [],
    allItems: [],
  });

  useEffect(() => {
    let active = true;
    const app = window.go?.main?.App;

    const refresh = async () => {
      try {
        const [sessionInfo, latestMessages, learningProgress] = await Promise.all([
          app?.GetSessionInfo?.(),
          app?.GetMessages?.(),
          app?.GetLearningProgress?.(),
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
        if (learningProgress) {
          setProgress({
            dueForReview: learningProgress.dueForReview || [],
            mastered: learningProgress.mastered || [],
            areasToWorkOn: learningProgress.areasToWorkOn || [],
            scheduled: learningProgress.scheduled || [],
            allItems: learningProgress.allItems || [],
          });
        }
      } catch (error) {
        console.error("could not load mentor session", error);
      }
    };

    void refresh();
    const interval = window.setInterval(() => void refresh(), 750);
    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, []);

  const dueCount = progress.dueForReview?.length ?? 0;
  const areasCount = progress.areasToWorkOn?.length ?? 0;
  const masteredCount = progress.mastered?.length ?? 0;
  const dueItems = progress.dueForReview || [];
  const areasItems = progress.areasToWorkOn || [];
  const masteredItems = progress.mastered || [];
  const scheduledItems = progress.scheduled || [];

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

        <nav className="tab-nav" style={{ marginTop: "24px", display: "flex", gap: "8px" }}>
          <button
            onClick={() => setActiveTab("guide")}
            style={{
              flex: 1,
              padding: "8px 12px",
              borderRadius: "8px",
              border: "1px solid",
              borderColor: activeTab === "guide" ? "#c4e0a2" : "#323431",
              background: activeTab === "guide" ? "#222a1e" : "#1d1e1f",
              color: activeTab === "guide" ? "#c4e0a2" : "#999e91",
              cursor: "pointer",
              fontWeight: 600,
              fontSize: "12px",
            }}
          >
            Study Guide
          </button>
          <button
            onClick={() => setActiveTab("chat")}
            style={{
              flex: 1,
              padding: "8px 12px",
              borderRadius: "8px",
              border: "1px solid",
              borderColor: activeTab === "chat" ? "#c4e0a2" : "#323431",
              background: activeTab === "chat" ? "#222a1e" : "#1d1e1f",
              color: activeTab === "chat" ? "#c4e0a2" : "#999e91",
              cursor: "pointer",
              fontWeight: 600,
              fontSize: "12px",
            }}
          >
            Dialogue
          </button>
        </nav>

        <div className="companion" style={{ marginTop: "20px" }}>
          <h2>Progress Summary</h2>
          <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: "8px" }}>
            <li style={{ display: "flex", justifyContent: "space-between", fontSize: "13px" }}>
              <span style={{ color: "#999e91" }}>Due for Review:</span>
              <strong style={{ color: dueCount > 0 ? "#e8c37d" : "#c4e0a2" }}>
                {dueCount}
              </strong>
            </li>
            <li style={{ display: "flex", justifyContent: "space-between", fontSize: "13px" }}>
              <span style={{ color: "#999e91" }}>Areas to Work On:</span>
              <strong style={{ color: areasCount > 0 ? "#ea8a8a" : "#999e91" }}>
                {areasCount}
              </strong>
            </li>
            <li style={{ display: "flex", justifyContent: "space-between", fontSize: "13px" }}>
              <span style={{ color: "#999e91" }}>Coming Up:</span>
              <strong style={{ color: "#9cc3e6" }}>{scheduledItems.length}</strong>
            </li>
            <li style={{ display: "flex", justifyContent: "space-between", fontSize: "13px" }}>
              <span style={{ color: "#999e91" }}>Mastered:</span>
              <strong style={{ color: "#c4e0a2" }}>{masteredCount}</strong>
            </li>
          </ul>

          <h2 style={{ marginTop: "20px" }}>Recent Questions</h2>
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
          <p>Reviews concepts that naturally come up based on your progress.</p>
        </div>

        <div className="sidebar-footer">
          <span className="linux-indicator" />
          <span>Linux CLI Mentor</span>
        </div>
      </aside>

      <section className="conversation" aria-label="Mentor Study Space">
        <header className="topbar">
          <div>
            <p className="eyebrow">YOUR LEARNING SPACE</p>
            <h1>{activeTab === "guide" ? "Study Guide & Reference Sheet" : "Mentor Chat"}</h1>
          </div>
          <div className="connection-pill">
            <span className="state-dot" />
            {conversationId ? "Session linked" : "Starting session"}
          </div>
        </header>

        {activeTab === "guide" ? (
          <div className="study-guide-view" style={{ padding: "28px 32px", overflowY: "auto", display: "flex", flexDirection: "column", gap: "28px" }}>
            {/* Commands Reference Sheet */}
            <div style={{ background: "#1a1b1d", border: "1px solid #2d2f32", borderRadius: "12px", padding: "20px" }}>
              <h2 style={{ margin: "0 0 14px", fontSize: "16px", color: "#c4e0a2" }}>
                Practiced Commands Cheat Sheet
              </h2>
              {commands.length === 0 ? (
                <p style={{ color: "#8a8f94", margin: 0, fontSize: "13px" }}>
                  Commands discussed in Antigravity will appear here automatically with one-click copy.
                </p>
              ) : (
                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))", gap: "10px" }}>
                  {commands.map((cmd) => (
                    <div
                      key={cmd}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        padding: "8px 12px",
                        background: "#141517",
                        border: "1px solid #28292a",
                        borderRadius: "8px",
                      }}
                    >
                      <code style={{ color: "#d1d5db", fontSize: "13px" }}>{cmd}</code>
                      <button
                        onClick={() => navigator.clipboard?.writeText(cmd)}
                        style={{
                          background: "#28292a",
                          border: "none",
                          color: "#c4e0a2",
                          borderRadius: "4px",
                          padding: "4px 8px",
                          fontSize: "11px",
                          cursor: "pointer",
                        }}
                      >
                        Copy
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Areas to Work On */}
            <div style={{ background: "#1a1b1d", border: "1px solid #2d2f32", borderRadius: "12px", padding: "20px" }}>
              <h2 style={{ margin: "0 0 14px", fontSize: "16px", color: "#ea8a8a" }}>
                Areas to Work On
              </h2>
              {areasCount === 0 ? (
                <p style={{ color: "#8a8f94", margin: 0, fontSize: "13px" }}>
                  No trouble spots. When an answer is wrong, the concept comes here until the next review goes cleanly.
                </p>
              ) : (
                <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: "10px" }}>
                  {areasItems.map((item) => (
                    <li
                      key={item.concept}
                      style={{
                        padding: "12px 14px",
                        background: "#201616",
                        border: "1px solid #4a2727",
                        borderRadius: "8px",
                      }}
                    >
                      <div style={{ display: "flex", justifyContent: "space-between", fontWeight: 600, color: "#f87171" }}>
                        <span>{item.concept}</span>
                      </div>
                      <p style={{ margin: "6px 0 0", color: "#e5b4b4", fontSize: "12px" }}>{describeProgress(item)}</p>
                      {item.notes && <p style={{ margin: "6px 0 0", color: "#d1d5db", fontSize: "12px" }}>{item.notes}</p>}
                      <p style={{ margin: "6px 0 0", color: "#b7a3a3", fontSize: "11px" }}>{describeAnswers(item)}</p>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {/* Spaced Repetition Due Queue */}
            <div style={{ background: "#1a1b1d", border: "1px solid #2d2f32", borderRadius: "12px", padding: "20px" }}>
              <h2 style={{ margin: "0 0 14px", fontSize: "16px", color: "#e8c37d" }}>
                Due for Review
              </h2>
              {dueCount === 0 ? (
                <p style={{ color: "#8a8f94", margin: 0, fontSize: "13px" }}>
                  Nothing to review today. Concepts appear here on their review date. See Coming Up for what's next.
                </p>
              ) : (
                <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: "10px" }}>
                  {dueItems.map((item) => (
                    <li
                      key={item.concept}
                      style={{
                        padding: "10px 14px",
                        background: "#1f1d18",
                        border: "1px solid #443c29",
                        borderRadius: "8px",
                        display: "flex",
                        justifyContent: "space-between",
                        alignItems: "center",
                      }}
                    >
                      <div>
                        <strong style={{ color: "#fde047" }}>{item.concept}</strong>
                        <span style={{ display: "block", color: "#999e91", fontSize: "11px" }}>
                          {describeProgress(item)}
                        </span>
                        <span style={{ display: "block", color: "#999e91", fontSize: "11px" }}>
                          {describeAnswers(item)}
                        </span>
                      </div>
                      <span style={{ fontSize: "12px", color: "#fef08a", background: "#443c29", padding: "3px 8px", borderRadius: "4px" }}>
                        Review Due
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {/* Scheduled: in progress, next review still ahead */}
            <div style={{ background: "#1a1b1d", border: "1px solid #2d2f32", borderRadius: "12px", padding: "20px" }}>
              <h2 style={{ margin: "0 0 14px", fontSize: "16px", color: "#9cc3e6" }}>Coming Up</h2>
              {scheduledItems.length === 0 ? (
                <p style={{ color: "#8a8f94", margin: 0, fontSize: "13px" }}>
                  Concepts waiting for their next review date show up here.
                </p>
              ) : (
                <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: "10px" }}>
                  {scheduledItems.map((item) => (
                    <li
                      key={item.concept}
                      style={{ padding: "10px 14px", background: "#171c21", border: "1px solid #29364a", borderRadius: "8px" }}
                    >
                      <strong style={{ color: "#bfdbfe" }}>{item.concept}</strong>
                      <span style={{ display: "block", color: "#9aa4b1", fontSize: "11px" }}>{describeProgress(item)}</span>
                      <span style={{ display: "block", color: "#9aa4b1", fontSize: "11px" }}>{describeAnswers(item)}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {/* Mastered Topics */}
            <div style={{ background: "#1a1b1d", border: "1px solid #2d2f32", borderRadius: "12px", padding: "20px" }}>
              <h2 style={{ margin: "0 0 14px", fontSize: "16px", color: "#c4e0a2" }}>
                Mastered (3 clean reviews in a row)
              </h2>
              {masteredCount === 0 ? (
                <p style={{ color: "#8a8f94", margin: 0, fontSize: "13px" }}>
                  Nothing mastered yet. A concept is mastered after three clean reviews in a row, each on its review date.
                </p>
              ) : (
                <div style={{ display: "flex", flexWrap: "wrap", gap: "8px" }}>
                  {masteredItems.map((item) => (
                    <span
                      key={item.concept}
                      style={{
                        padding: "6px 12px",
                        background: "#1c2619",
                        border: "1px solid #2f4528",
                        borderRadius: "6px",
                        color: "#86efac",
                        fontSize: "12px",
                        fontWeight: 600,
                      }}
                    >
                      ✓ {item.concept}
                    </span>
                  ))}
                </div>
              )}
            </div>

            {/* How the review schedule works (matches intervalDays in internal/progress/store.go) */}
            <div style={{ background: "#1a1b1d", border: "1px solid #2d2f32", borderRadius: "12px", padding: "20px" }}>
              <h2 style={{ margin: "0 0 10px", fontSize: "16px", color: "#c9ccd1" }}>How reviews work</h2>
              <p style={{ color: "#8a8f94", margin: 0, fontSize: "13px", lineHeight: 1.5 }}>
                A concept comes back after 14, 28, then 56 days if you've never missed it. After 1 miss it's
                2, 4, 8 days, and after 2 or more misses it's 1, 2, 4 days. A wrong answer starts the count
                over. Three clean reviews in a row master it. Correct answers before a review date count as
                practice only.
              </p>
            </div>
          </div>
        ) : (
          <div className={`message-area${messages.length ? " has-messages" : ""}`}>
            {messages.length === 0 ? (
              <div className="welcome">
                <div className="welcome-mark" aria-hidden="true">
                  <TerminalIcon />
                </div>
                <p className="eyebrow">THINK IT THROUGH</p>
                <h2>A mentor to help you learn faster and more efficiently.</h2>
                <p className="welcome-copy">
                  Your mentor’s explanations and questions appear here. Use Antigravity in
                  the terminal to practice commands and reasoning.
                </p>
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
                      <p>{cleanContent(message.content)}</p>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </div>
        )}

        <footer className="conversation-footer">
          <span className="footer-orbit" aria-hidden="true">
            <SparkleIcon />
          </span>
          <span>
            {activeTab === "guide"
              ? "Reference sheet and spaced repetition updates refresh automatically from your practice."
              : "Keep exploring in your terminal. Your mentor’s replies will appear here."}
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
