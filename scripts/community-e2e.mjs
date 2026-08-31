#!/usr/bin/env node

import {mkdtemp, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {spawn} from "node:child_process";
import {createServer} from "node:net";

const timeoutMs = Number.parseInt(process.env.TORGNEXA_E2E_TIMEOUT_MS ?? "30000", 10);
const appPort = portFromEnv("TORGNEXA_FRONTEND_PORT", 5173);
const keycloakPort = portFromEnv("KEYCLOAK_PORT", 8081);
const appURL = process.env.TORGNEXA_E2E_BASE_URL ?? `http://127.0.0.1:${appPort}`;
const keycloakURL = process.env.TORGNEXA_E2E_KEYCLOAK_URL ?? `http://127.0.0.1:${keycloakPort}`;
const username = process.env.TORGNEXA_DEMO_USERNAME ?? "demo";
const password = process.env.TORGNEXA_DEMO_PASSWORD ?? "demo-local-only";
const chromeBinary = process.env.CHROME_BIN ?? "google-chrome";

function portFromEnv(name, fallback) {
  if (process.env[name] && /^[0-9]+$/.test(process.env[name])) return Number(process.env[name]);
  try {
    const text = requireUnavailableRead(name);
    const match = text.match(new RegExp(`^${name}=([0-9]+)$`, "m"));
    return match ? Number(match[1]) : fallback;
  } catch {
    return fallback;
  }
}

// This synchronous helper only reads the two non-secret port settings from
// .env. Secrets are deliberately not parsed or exported by the browser test.
function requireUnavailableRead(name) {
  const fs = process.getBuiltinModule?.("node:fs");
  if (!fs) throw new Error(`cannot inspect ${name}`);
  return fs.readFileSync(".env", "utf8");
}

function fail(message) {
  throw new Error(`community-e2e: ${message}`);
}

function assert(condition, message) {
  if (!condition) fail(message);
}

async function waitFor(label, operation, limit = timeoutMs) {
  const deadline = Date.now() + limit;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const value = await operation();
      if (value) return value;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  fail(`ожидание «${label}» завершилось по таймауту${lastError ? `: ${lastError.message}` : ""}`);
}

async function checkEndpoint(url, label) {
  await waitFor(label, async () => {
    const response = await fetch(url, {redirect: "error"});
    return response.ok;
  });
}

async function freePort() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  await new Promise((resolve) => server.close(resolve));
  assert(port > 0, "не удалось выбрать порт удалённой отладки Chrome");
  return port;
}

async function debugJSON(debugURL, path) {
  const response = await fetch(`${debugURL}${path}`, {redirect: "error"});
  if (!response.ok) fail(`Chrome DevTools вернул HTTP ${response.status} для ${path}`);
  return await response.json();
}

class CdpSession {
  constructor(webSocketURL) {
    this.socket = new WebSocket(webSocketURL);
    this.nextID = 1;
    this.pending = new Map();
    this.open = new Promise((resolve, reject) => {
      this.socket.addEventListener("open", resolve, {once: true});
      this.socket.addEventListener("error", reject, {once: true});
    });
    this.socket.addEventListener("message", (event) => {
      const message = JSON.parse(String(event.data));
      if (!message.id) return;
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      if (message.error) pending.reject(new Error(message.error.message ?? "CDP command failed"));
      else pending.resolve(message.result ?? {});
    });
  }

  async send(method, params = {}) {
    await this.open;
    const id = this.nextID++;
    const promise = new Promise((resolve, reject) => this.pending.set(id, {resolve, reject}));
    this.socket.send(JSON.stringify({id, method, params}));
    return await promise;
  }

  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true, userGesture: true});
    if (result.exceptionDetails) {
      const description = result.exceptionDetails.exception?.description ?? result.exceptionDetails.text ?? "ошибка JavaScript";
      fail(`ошибка страницы: ${description}`);
    }
    return result.result?.value;
  }

  async waitFor(expression, label, limit = timeoutMs) {
    return await waitFor(label, () => this.evaluate(expression), limit);
  }

  async close() {
    try { this.socket.close(); } catch { /* already closed */ }
  }
}

async function listPages(debugURL) {
  return (await debugJSON(debugURL, "/json/list")).filter((target) => target.type === "page");
}

async function connectPage(debugURL, predicate, label) {
  const target = await waitFor(label, async () => (await listPages(debugURL)).find(predicate));
  assert(target.webSocketDebuggerUrl, `${label}: отсутствует WebSocket CDP`);
  const page = new CdpSession(target.webSocketDebuggerUrl);
  await page.send("Page.enable");
  await page.send("Runtime.enable");
  return {page, target};
}

async function navigate(page, url) {
  await page.send("Page.navigate", {url});
  await page.waitFor("document.readyState === 'complete'", `загрузка ${url}`);
}

async function click(page, selector, label) {
  const expression = `(() => { const element = document.querySelector(${JSON.stringify(selector)}); if (!element) return false; element.click(); return true; })()`;
  assert(await page.waitFor(expression, label), `${label}: элемент не найден`);
}

async function clickText(page, selector, text, label) {
  const expression = `(() => { const element = [...document.querySelectorAll(${JSON.stringify(selector)})].find((candidate) => candidate.textContent?.trim() === ${JSON.stringify(text)}); if (!element) return false; element.click(); return true; })()`;
  assert(await page.waitFor(expression, label), `${label}: элемент не найден`);
}

async function fillFormInputs(page, selector, values, label) {
  const expression = `document.querySelectorAll(${JSON.stringify(selector)}).length`;
  const count = await page.evaluate(expression);
  assert(count === values.length, `${label}: ожидалось ${values.length} полей, найдено ${count}`);
  for (let index = 0; index < values.length; index += 1) {
    const inputType = await page.evaluate(`(() => {
      const input = document.querySelectorAll(${JSON.stringify(selector)})[${index}];
      if (!input) return '';
      input.focus();
      input.select?.();
      return input.type;
    })()`);
    assert(inputType, `${label}: поле ${index + 1} не найдено`);
    if (inputType === "date") {
      await page.evaluate(`(() => {
        const input = document.querySelectorAll(${JSON.stringify(selector)})[${index}];
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
        setter?.call(input, ${JSON.stringify(values[index])});
        input.dispatchEvent(new Event('input', {bubbles: true}));
        input.dispatchEvent(new Event('change', {bubbles: true}));
      })()`);
    } else {
      await page.send("Input.insertText", {text: String(values[index])});
    }
  }
  await page.evaluate("document.activeElement?.blur()");
  await page.waitFor(`(() => {
    const inputs = [...document.querySelectorAll(${JSON.stringify(selector)})];
    const values = ${JSON.stringify(values)};
    return inputs.length === values.length && values.every((value, index) => inputs[index]?.value === value);
  })()`, label, 5000);
}

async function capture(page, path) {
  try {
    const screenshot = await page.send("Page.captureScreenshot", {format: "png", captureBeyondViewport: true});
    await writeFile(path, Buffer.from(screenshot.data, "base64"));
  } catch {
    // A diagnostic screenshot must never hide the original test failure.
  }
}

async function launchChrome(debugPort, profileDir) {
  const args = [
    "--headless=new",
    "--no-sandbox",
    "--disable-gpu",
    "--disable-dev-shm-usage",
    "--disable-popup-blocking",
    "--no-first-run",
    "--no-default-browser-check",
    "--password-store=basic",
    "--lang=ru-RU",
    `--window-size=1440,1100`,
    `--remote-debugging-port=${debugPort}`,
    `--user-data-dir=${profileDir}`,
    "about:blank",
  ];
  const child = spawn(chromeBinary, args, {stdio: ["ignore", "ignore", "pipe"]});
  let startupError = null;
  child.once("error", (error) => { startupError = error; });
  const debugURL = `http://127.0.0.1:${debugPort}`;
  try {
    await waitFor("удалённый Chrome", async () => {
      try {
        await debugJSON(debugURL, "/json/version");
        return true;
      } catch (error) {
        if (startupError) throw startupError;
        if (child.exitCode !== null) throw new Error(`Chrome завершился с кодом ${child.exitCode}`);
        return false;
      }
    }, 15000);
  } catch (error) {
    child.kill("SIGTERM");
    if (error.code === "ENOENT") fail(`не найден Chrome (${chromeBinary}); задайте CHROME_BIN`);
    throw error;
  }
  return {child, debugURL};
}

async function stopChrome(child) {
  if (child.exitCode !== null) return;
  const exited = new Promise((resolve) => child.once("exit", resolve));
  child.kill("SIGTERM");
  await Promise.race([exited, new Promise((resolve) => setTimeout(resolve, 3000))]);
  if (child.exitCode === null) child.kill("SIGKILL");
  await Promise.race([exited, new Promise((resolve) => setTimeout(resolve, 1000))]);
}

async function login(page, debugURL) {
  await page.waitFor("document.body?.innerText.includes('Войти')", "экран входа TORGNEXA");
  const before = new Set((await listPages(debugURL)).map((target) => target.id));
  await clickText(page, "button", "Войти", "кнопка входа");
  const popup = await connectPage(debugURL, (target) => !before.has(target.id), "окно Keycloak");
  await popup.page.waitFor("Boolean(document.querySelector('#username, input[name=username]'))", "форма Keycloak");
  const values = `(() => {
    const set = (selector, value) => {
      const element = document.querySelector(selector);
      if (!element) return false;
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
      setter?.call(element, value);
      element.dispatchEvent(new Event('input', {bubbles: true}));
      element.dispatchEvent(new Event('change', {bubbles: true}));
      return true;
    };
    return {username: set('#username, input[name=username]', ${JSON.stringify(username)}), password: set('#password, input[name=password]', ${JSON.stringify(password)})};
  })()`;
  const filled = await popup.page.evaluate(values);
  assert(filled?.username && filled?.password, "не удалось заполнить форму Keycloak");
  await click(popup.page, "#kc-login, button[type=submit], input[type=submit]", "кнопка входа Keycloak");
  await page.waitFor("document.querySelector('.sidebar') && document.body.innerText.includes('Каталог')", "авторизованная консоль", 45000);
  await popup.page.close();
}

async function run() {
  await checkEndpoint(`${appURL}/`, "frontend");
  await checkEndpoint(`${keycloakURL}/realms/torgnexa/.well-known/openid-configuration`, "Keycloak realm");
  const debugPort = await freePort();
  const profileDir = await mkdtemp(join(tmpdir(), "torgnexa-community-e2e-"));
  const artifactDir = process.env.TORGNEXA_E2E_ARTIFACT_DIR ?? join(tmpdir(), "torgnexa-community-e2e-artifacts");
  const screenshotPath = join(artifactDir, "failure.png");
  const {child, debugURL} = await launchChrome(debugPort, profileDir);
  let page;
  try {
    const connected = await connectPage(debugURL, (target) => target.url === "about:blank");
    page = connected.page;
    await navigate(page, appURL);
    await login(page, debugURL);
    const session = await page.evaluate("({url: location.href, display: document.querySelector('.profile-copy strong')?.textContent ?? ''})");
    assert(session?.url === new URL("/", appURL).toString(), `после входа открыта неожиданная страница: ${session?.url}`);
    assert(/Демо|Demo/.test(session.display), "в консоли не отображается демо-пользователь");

    // Publication quality is a read-only, tenant-scoped surface. Check it
    // immediately after authentication so a missing migration or stale API
    // image cannot regress into a generic loading error in the console.
    await navigate(page, new URL("/publication-quality", appURL).toString());
    await page.waitFor("document.body.innerText.includes('Качество публикации')", "центр качества публикации", 45000);
    await page.waitFor("!document.body.innerText.includes('Не удалось загрузить центр качества публикации.')", "ответ API центра качества", 45000);
    await navigate(page, appURL);
    await page.waitFor("document.querySelector('.sidebar') && document.body.innerText.includes('Каталог')", "возврат в консоль", 45000);
    if (process.env.TORGNEXA_E2E_PUBLICATION_QUALITY_ONLY === "true") {
      console.log("community-e2e: PASS — Keycloak demo user and publication quality center verified");
      return;
    }

    // Keep the browser check self-contained on a fresh or previously reused
    // community stack. The action is idempotent and only creates synthetic
    // tenant-scoped records; it also repairs a stale demo image projection.
    await clickText(page, "button", "Заполнить весь демо-контур", "запуск демо-контура");
    await page.waitFor("document.querySelector('.toast-success')?.innerText.includes('Демо-контур заполнен')", "завершение демо-контура", 45000);

    await clickText(page, ".nav-item", "Настройки", "переход в настройки профиля");
    await page.waitFor("location.pathname === '/settings'", "маршрут профиля");
    await page.waitFor("Boolean(document.querySelector('.profile-card'))", "карточка профиля", 45000);
    await page.waitFor("document.querySelector('.profile-card')?.innerText.toLocaleLowerCase('ru-RU').includes('профиль пользователя')", "заголовок профиля", 45000);
    await page.waitFor("document.querySelector('.profile-card')?.innerText.includes('Старший операционный менеджер') && document.querySelector('.profile-card')?.innerText.includes('Коммерческие операции')", "должность и подразделение профиля", 45000);
    await page.waitFor("document.querySelector('.profile-card')?.innerText.includes('demo@local.torgnexa') && document.querySelector('.profile-card')?.innerText.includes('Дата рождения')", "контактные данные профиля", 45000);
    await page.waitFor("(() => { const image = document.querySelector('.profile-card .user-avatar img'); return Boolean(image) && image.complete && image.naturalWidth > 0; })()", "аватар профиля", 45000);
    await clickText(page, ".profile-card button", "Изменить профиль", "редактирование профиля");
    await fillFormInputs(page, ".profile-editor input", ["Демо-проверка", "Оператор", "1988-04-17", "Старший операционный менеджер", "Коммерческие операции", "+7 (495) 555-01-43"], "заполнение формы профиля");
    await clickText(page, ".profile-editor button", "Сохранить профиль", "сохранение профиля");
    await page.waitFor("document.querySelector('.profile-card h2')?.innerText.includes('Демо-проверка Оператор') && document.querySelector('.profile-card')?.innerText.includes('+7 (495) 555-01-43')", "сохранённые данные профиля", 45000);
    await clickText(page, ".profile-card button", "Изменить профиль", "возврат профиля к демо-значениям");
    await fillFormInputs(page, ".profile-editor input", ["Демо", "Оператор", "1988-04-17", "Старший операционный менеджер", "Коммерческие операции", "+7 (495) 555-01-42"], "подготовка восстановления профиля");
    await clickText(page, ".profile-editor button", "Сохранить профиль", "восстановление демо-профиля");
    await page.waitFor("document.querySelector('.profile-card h2')?.innerText.includes('Демо Оператор')", "восстановленный профиль", 45000);

    // The admin member surface uses the same authenticated session. Select the
    // demo member explicitly, edit the server-backed profile, verify the
    // rendered result, and restore the synthetic values before continuing.
    await page.waitFor("Boolean(document.querySelector('.member-settings tbody tr'))", "список участников workspace", 45000);
    const selectedMember = await page.waitFor(`(() => {
      const row = [...document.querySelectorAll('.member-settings tbody tr')].find((candidate) => candidate.innerText.includes('demo@local.torgnexa'));
      if (!row) return false;
      const button = [...row.querySelectorAll('button')].find((candidate) => candidate.textContent?.trim() === 'Профиль');
      if (!button) return false;
      button.click();
      return true;
    })()`, "открытие профиля участника демо-контура", 45000);
    assert(selectedMember, "в списке workspace не найден демо-участник с профилем");
    await page.waitFor("Boolean(document.querySelector('.member-profile-editor input'))", "форма профиля участника", 45000);
    await fillFormInputs(page, ".member-profile-editor input", ["Демо-администратор", "Оператор", "1988-04-17", "Руководитель демонстрационного контура", "Коммерческие операции", "+7 (495) 555-01-44"], "подготовка изменения профиля участника");
    await clickText(page, ".member-profile-editor button", "Сохранить профиль", "сохранение профиля участника администратором");
    await page.waitFor(`(() => {
      const inputs = [...document.querySelectorAll('.member-profile-editor input')];
      return inputs[0]?.value === 'Демо-администратор' && inputs[3]?.value === 'Руководитель демонстрационного контура' && inputs[5]?.value === '+7 (495) 555-01-44';
    })()`, "проверка изменённого профиля участника", 45000);
    await page.waitFor(`(() => {
      const button = [...document.querySelectorAll('.member-profile-editor button')].find((candidate) => candidate.textContent?.trim() === 'Сохранить профиль');
      return Boolean(button) && !button.disabled;
    })()`, "завершение сохранения профиля участника", 45000);
    await fillFormInputs(page, ".member-profile-editor input", ["Демо", "Оператор", "1988-04-17", "Старший операционный менеджер", "Коммерческие операции", "+7 (495) 555-01-42"], "подготовка восстановления профиля участника");
    await clickText(page, ".member-profile-editor button", "Сохранить профиль", "восстановление профиля участника демо-контура");
    await page.waitFor(`(() => {
      const inputs = [...document.querySelectorAll('.member-profile-editor input')];
      return inputs[0]?.value === 'Демо' && inputs[3]?.value === 'Старший операционный менеджер' && inputs[5]?.value === '+7 (495) 555-01-42';
    })()`, "проверка восстановленного профиля участника", 45000);

    // The administrator edited the same synthetic account that is logged in.
    // Reload Settings so the current-user profile query receives the new
    // optimistic-concurrency version before avatar/privacy actions continue.
    await navigate(page, `${appURL}/settings`);
    await page.waitFor("location.pathname === '/settings' && document.querySelector('.profile-card h2')?.innerText.includes('Демо Оператор') && document.querySelector('.profile-card')?.innerText.includes('+7 (495) 555-01-42')", "свежая версия восстановленного профиля", 45000);
    await clickText(page, ".profile-privacy-buttons button", "Запросить выгрузку", "запрос выгрузки профиля");
    await page.waitFor("document.body.innerText.includes('Запрос принят и поставлен в очередь privacy-workflow.')", "постановка privacy-запроса в очередь", 45000);

    const avatarFile = await page.evaluate(`(() => {
      const input = document.querySelector(".profile-avatar-actions input[type=file]");
      if (!input) return false;
      const encoded = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=";
      const bytes = Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0));
      const transfer = new DataTransfer();
      transfer.items.add(new File([bytes], "profile-avatar.png", {type: "image/png"}));
      input.files = transfer.files;
      input.dispatchEvent(new Event("change", {bubbles: true}));
      return true;
    })()`);
    assert(avatarFile, "не удалось передать тестовый аватар");
    await page.waitFor("document.querySelector('.profile-avatar-actions')?.innerText.includes('Удалить фото') && (() => { const image = document.querySelector('.profile-card .user-avatar img'); return Boolean(image) && image.complete && image.naturalWidth > 0; })()", "загрузка пользовательского аватара", 60000);
    await page.waitFor("(() => { const button = [...document.querySelectorAll('.profile-avatar-actions button')].find((candidate) => candidate.textContent?.trim() === 'Удалить фото'); return Boolean(button) && !button.disabled; })()", "готовность удаления пользовательского аватара", 45000);
    await clickText(page, ".profile-avatar-actions button", "Удалить фото", "удаление пользовательского аватара");
    await page.waitFor("!document.querySelector('.profile-avatar-actions')?.innerText.includes('Удалить фото')", "удалённый пользовательский аватар", 45000);

    await clickText(page, ".nav-item", "Каталог", "переход в каталог");
    await page.waitFor("location.pathname === '/catalog'", "маршрут каталога");
    const catalog = await page.waitFor("(() => { const rows = document.querySelectorAll('.server-grid tbody tr').length; const images = [...document.querySelectorAll('img.catalog-product-thumbnail')]; return rows > 0 && images.length > 0 && images.some((image) => image.complete && image.naturalWidth > 0); })()", "демо-каталог и миниатюры", 45000);
    assert(catalog, "каталог не показал товары с загруженными миниатюрами");
    await click(page, ".server-grid tbody tr:first-child button[aria-label='Открыть']", "открытие карточки товара");
    await page.waitFor("Boolean(document.querySelector('[role=dialog] .catalog-primary-image img'))", "главное изображение карточки товара");
    await clickText(page, ".catalog-tabs button", "Изображения", "вкладка изображений карточки");
    await page.waitFor("document.querySelectorAll('.catalog-image-editor img').length > 0", "изображения в карточке товара");

    await click(page, ".drawer-header button[aria-label='Закрыть']", "закрытие карточки товара");
    await clickText(page, ".nav-item", "Заказы", "переход в заказы");
    await page.waitFor("location.pathname === '/orders'", "маршрут заказов");
    await page.waitFor("document.querySelectorAll('img.order-product-thumbnail').length > 0", "миниатюра товара в заказе", 45000);
    const order = await page.evaluate("({rows: document.querySelectorAll('.server-grid tbody tr').length, loaded: [...document.querySelectorAll('img.order-product-thumbnail')].some((image) => image.complete && image.naturalWidth > 0), source: document.querySelector('img.order-product-thumbnail')?.getAttribute('src') ?? ''})");
    assert(order.rows > 0 && order.loaded, "заказы не показали загруженную миниатюру товара");
    assert(order.source.includes("demo-images") || order.source.startsWith("/api/v1/uploads/"), "заказ не содержит адрес изображения товара");
    const pendingOrder = await page.waitFor("(() => { const row = [...document.querySelectorAll('.server-grid tbody tr')].find((candidate) => candidate.innerText.includes('Ожидает')); if (!row) return false; const button = row.querySelector(\"button[aria-label='Открыть']\"); if (!button) return false; button.click(); return true; })()", "выбор ожидающего заказа", 45000);
    assert(pendingOrder, "в демо-контуре нет заказа в статусе «Ожидают» для проверки действий");
    await page.waitFor("document.querySelector('[role=dialog]')?.innerText.includes('Позиции заказа')", "позиции заказа");
    await page.waitFor("Boolean(document.querySelector('[role=dialog] img.order-product-thumbnail'))", "миниатюра товара в деталях заказа");
    const actions = await page.evaluate("[...document.querySelectorAll('[role=dialog] button')].map((button) => button.textContent?.trim()).filter(Boolean).join(' | ')");
    assert(actions.includes("Подтвердить заказ") || actions.includes("Отменить заказ"), "в карточке заказа не отображаются доступные действия");
    console.log("community-e2e: PASS — Keycloak demo user, profile editing, privacy request, catalog, product images, orders and order thumbnail verified");
  } catch (error) {
    await mkdirIfNeeded(artifactDir);
    if (page) {
      try {
        const state = await page.evaluate("(() => { const card = document.querySelector('.profile-card'); const image = card?.querySelector('.user-avatar img'); return {path: location.pathname, text: card?.innerText ?? '', image: image ? {src: image.getAttribute('src'), complete: image.complete, naturalWidth: image.naturalWidth} : null}; })()");
        console.error(`community-e2e: состояние перед сбоем: ${JSON.stringify(state)}`);
      } catch { /* diagnostic state is best effort */ }
    }
    if (page) await capture(page, screenshotPath);
    console.error(`community-e2e: FAIL — ${error.message}`);
    console.error(`community-e2e: диагностический снимок: ${screenshotPath}`);
    process.exitCode = 1;
  } finally {
    await page?.close();
    await stopChrome(child);
    try { await rm(profileDir, {recursive: true, force: true, maxRetries: 5, retryDelay: 200}); } catch { /* diagnostics already reported */ }
  }
}

async function mkdirIfNeeded(path) {
  const fs = process.getBuiltinModule?.("node:fs");
  fs?.mkdirSync(path, {recursive: true});
}

try {
  await run();
} catch (error) {
  console.error(`community-e2e: FAIL — ${error instanceof Error ? error.message : String(error)}`);
  process.exitCode = 1;
}
