import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { UNAUTHORIZED_EVENT, saveAccessToken } from "./api";

type Props = {
  children: ReactNode;
  // 保存令牌后的动作；默认整页刷新，让所有页面用新令牌重新取数。
  onUnlock?: () => void;
};

export function AccessGate({
  children,
  onUnlock = () => window.location.reload(),
}: Props) {
  const [locked, setLocked] = useState(false);
  const [token, setToken] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    const lock = () => setLocked(true);
    window.addEventListener(UNAUTHORIZED_EVENT, lock);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, lock);
  }, []);

  if (!locked) return <>{children}</>;

  function submit(event: FormEvent) {
    event.preventDefault();
    const value = token.trim();
    if (!value) {
      setError("请输入访问令牌。");
      return;
    }
    saveAccessToken(value);
    onUnlock();
  }

  return (
    <main className="access-gate">
      <form onSubmit={submit} aria-labelledby="access-gate-title">
        <h1 id="access-gate-title">需要访问令牌</h1>
        <p>此 TRadar 实例已开启访问保护。令牌只保存在当前标签页，关闭后需重新输入。</p>
        <label htmlFor="access-token">访问令牌</label>
        <input
          id="access-token"
          type="password"
          autoComplete="off"
          autoFocus
          value={token}
          onChange={(event) => setToken(event.target.value)}
          aria-describedby={error ? "access-token-error" : undefined}
        />
        {error && (
          <p id="access-token-error" role="alert" className="access-gate-error">
            {error}
          </p>
        )}
        <button type="submit" className="primary">
          解锁
        </button>
      </form>
    </main>
  );
}
