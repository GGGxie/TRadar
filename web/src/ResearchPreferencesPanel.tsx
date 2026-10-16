import { useEffect, useState } from "react";
import {
  ApiError,
  getResearchPreferences,
  updateResearchPreferences,
  type ResearchPreferences,
} from "./api";

function topics(value: string) {
  return value
    .split(/[，,\n]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function ResearchPreferencesPanel() {
  const [current, setCurrent] = useState<ResearchPreferences | null>(null);
  const [topicText, setTopicText] = useState("");
  const [daily, setDaily] = useState(3);
  const [weekly, setWeekly] = useState(2);
  const [monthly, setMonthly] = useState(1);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    getResearchPreferences(controller.signal)
      .then((result) => {
        setCurrent(result.data);
        setTopicText(result.data.focusTopics.join("\n"));
        setDaily(result.data.dailySaveTarget);
        setWeekly(result.data.weeklyDeepTarget);
        setMonthly(result.data.monthlyPracticeTarget);
      })
      .catch((problem) => {
        if (!controller.signal.aborted)
          setError(
            problem instanceof Error ? problem.message : "读取研究偏好失败",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    const protect = (event: BeforeUnloadEvent) => {
      if (dirty) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", protect);
    return () => window.removeEventListener("beforeunload", protect);
  }, [dirty]);

  function change(action: () => void) {
    action();
    setDirty(true);
    setError("");
    setMessage("");
  }

  async function save() {
    if (!current || saving) return;
    setSaving(true);
    setError("");
    setMessage("");
    try {
      const result = await updateResearchPreferences(current, {
        focusTopics: topics(topicText),
        dailySaveTarget: daily,
        weeklyDeepTarget: weekly,
        monthlyPracticeTarget: monthly,
      });
      setCurrent(result.data);
      setTopicText(result.data.focusTopics.join("\n"));
      setDaily(result.data.dailySaveTarget);
      setWeekly(result.data.weeklyDeepTarget);
      setMonthly(result.data.monthlyPracticeTarget);
      setDirty(false);
      setMessage("研究偏好已保存");
    } catch (problem) {
      if (problem instanceof ApiError && problem.status === 412) {
        setError(
          "研究偏好已在其他页面变化。当前输入已保留，请刷新后手工核对。",
        );
      } else {
        setError(
          problem instanceof Error ? problem.message : "保存研究偏好失败",
        );
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <section
      className="research-preferences"
      aria-labelledby="research-preferences-title"
    >
      <div className="section-heading">
        <div>
          <p className="eyebrow">RESEARCH RHYTHM</p>
          <h2 id="research-preferences-title">关注方向与研究建议</h2>
          <p>
            建议值只用于提示节奏，不会限制收藏、深入研究或实践。设为 0
            可隐藏相应建议。
          </p>
        </div>
      </div>
      {loading ? (
        <p className="loading">正在读取研究偏好…</p>
      ) : !current ? (
        <p role="alert" className="error">
          {error || "研究偏好暂时不可用"}
        </p>
      ) : (
        <div className="research-preferences-form">
          <label className="focus-topics-field">
            <span>关注方向</span>
            <textarea
              rows={3}
              value={topicText}
              disabled={saving}
              onChange={(event) =>
                change(() => setTopicText(event.target.value))
              }
            />
          </label>
          <label>
            <span>每日收藏建议</span>
            <input
              type="number"
              min="0"
              max="100"
              value={daily}
              disabled={saving}
              onChange={(event) =>
                change(() => setDaily(Number(event.target.value)))
              }
            />
          </label>
          <label>
            <span>每周深入研究建议</span>
            <input
              type="number"
              min="0"
              max="100"
              value={weekly}
              disabled={saving}
              onChange={(event) =>
                change(() => setWeekly(Number(event.target.value)))
              }
            />
          </label>
          <label>
            <span>每月实践建议</span>
            <input
              type="number"
              min="0"
              max="100"
              value={monthly}
              disabled={saving}
              onChange={(event) =>
                change(() => setMonthly(Number(event.target.value)))
              }
            />
          </label>
          <button
            className="secondary"
            disabled={saving || !dirty}
            onClick={save}
          >
            {saving ? "正在保存…" : "保存研究偏好"}
          </button>
        </div>
      )}
      {error && current && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {message && (
        <p role="status" className="success-message">
          {message}
        </p>
      )}
    </section>
  );
}
