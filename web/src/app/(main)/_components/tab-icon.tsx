"use client";

import { useEffect } from "react";

// 页签图标只属于已登录界面。不能改用 app/icon.png、app/favicon.ico 这类静态文件约定：
// 那会写进每一页的 <head>，包括未登录的登录页，而图片哈希是最容易被测绘的指纹
// （同 login/page.tsx 的防指纹约定）。上游的图标正是这样被带到每一个页面的。
export function TabIcon() {
  useEffect(() => {
    const link = document.createElement("link");
    link.rel = "icon";
    link.type = "image/png";
    link.href = "/logo.png";
    document.head.appendChild(link);
    return () => {
      link.remove();
    };
  }, []);

  return null;
}
