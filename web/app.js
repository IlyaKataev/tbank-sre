'use strict';

(() => {
  const $ = (id) => document.getElementById(id);
  const storageKey = 'marketplace-session';
  const statuses = { ACTIVE: ['В продаже', 'active'], INACTIVE: ['Скрыт', 'inactive'], ARCHIVED: ['В архиве', 'archived'] };
  const money = new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB', maximumFractionDigits: 2 });
  const number = new Intl.NumberFormat('ru-RU');
  const state = { session: null, user: null, register: false, page: 0, size: 10, status: '', category: '', items: [], total: 0, loading: false, listVersion: 0, statsVersion: 0, authVersion: 0, product: null, archive: null, productMode: 'create', saving: false };
  let refreshPending = null;
  let toastTimeout;

  function escapeHTML(value) {
    return String(value ?? '').replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
  }

  function claims(token) {
    try {
      const payload = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
      return JSON.parse(atob(payload));
    } catch { return null; }
  }

  function storeSession(session) {
    state.session = session;
    state.user = session ? claims(session.access_token) : null;
    try {
      if (session) sessionStorage.setItem(storageKey, JSON.stringify(session));
      else sessionStorage.removeItem(storageKey);
    } catch { /* The current session still works when browser storage is unavailable. */ }
  }

  function setError(id, message = '') {
    $(id).textContent = message;
    $(id).hidden = !message;
  }

  function notify(message) {
    clearTimeout(toastTimeout);
    $('toast').querySelector('span').textContent = message;
    $('toast').hidden = false;
    toastTimeout = setTimeout(() => { $('toast').hidden = true; }, 4000);
  }

  function isSeller() { return state.user?.role === 'SELLER' || state.user?.role === 'ADMIN'; }
  function canEdit(product) { return state.user?.role === 'ADMIN' || (isSeller() && product.seller_id === state.user?.sub); }

  class RequestError extends Error {
    constructor(message, status = 0, code = '') { super(message); this.status = status; this.code = code; }
  }

  async function request(path, { method = 'GET', body, token } = {}) {
    const headers = { Accept: 'application/json' };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (token) headers.Authorization = `Bearer ${token}`;
    let response;
    try {
      response = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), cache: 'no-store' });
    } catch { throw new RequestError('Не удалось связаться с сервером. Проверьте соединение и попробуйте ещё раз.'); }
    let data;
    try { data = await response.json(); } catch { throw new RequestError('Сервер вернул неожиданный ответ. Попробуйте ещё раз.', response.status); }
    if (!response.ok) {
      const messages = {
        INVALID_CREDENTIALS: 'Неверная почта или пароль. Попробуйте ещё раз.',
        USER_ALREADY_EXISTS: 'Аккаунт с этой почтой уже существует. Перейдите на вкладку «Войти».',
        ACCESS_DENIED: 'Изменять этот товар может только его продавец.',
        PRODUCT_NOT_FOUND: 'Товар не найден. Обновите каталог.',
        TOKEN_EXPIRED: 'Время входа истекло. Войдите снова.',
        TOKEN_INVALID: 'Войдите в аккаунт, чтобы продолжить.',
        REFRESH_TOKEN_INVALID: 'Время входа истекло. Войдите снова.',
        VALIDATION_ERROR: 'Проверьте заполненные поля: почту, пароль и значения товара.',
      };
      const message = messages[data.error_code] || (response.status >= 500 ? 'Сервис временно недоступен. Попробуйте ещё раз чуть позже.' : data.message) || 'Не удалось выполнить действие.';
      throw new RequestError(message, response.status, data.error_code);
    }
    return data;
  }

  async function refreshSession() {
    if (!refreshPending) {
      const previous = state.session;
      const version = state.authVersion;
      if (!previous?.refresh_token) throw new RequestError('Войдите в аккаунт, чтобы продолжить.', 401);
      refreshPending = request('/auth/refresh', { method: 'POST', body: { refresh_token: previous.refresh_token } })
        .then((tokens) => {
          if (version !== state.authVersion) throw new RequestError('Аккаунт был изменён. Повторите действие.', 401);
          storeSession({ ...tokens, email: previous.email });
        })
        .catch((error) => {
          if (error.status === 401 && version === state.authVersion) logout('Время входа истекло. Войдите снова.');
          throw error;
        })
        .finally(() => { refreshPending = null; });
    }
    return refreshPending;
  }

  async function api(path, options = {}) {
    if (!state.session) throw new RequestError('Войдите в аккаунт, чтобы продолжить.', 401);
    const version = state.authVersion;
    if ((state.user?.exp ?? 0) * 1000 <= Date.now()) await refreshSession();
    try {
      return await request(path, { ...options, token: state.session?.access_token });
    } catch (error) {
      if (error.status !== 401 || version !== state.authVersion) throw error;
      await refreshSession();
      return request(path, { ...options, token: state.session?.access_token });
    }
  }

  function setAuthMode(register) {
    state.register = register;
    $('login-tab').classList.toggle('selected', !register);
    $('register-tab').classList.toggle('selected', register);
    $('login-tab').setAttribute('aria-pressed', String(!register));
    $('register-tab').setAttribute('aria-pressed', String(register));
    $('auth-title').textContent = register ? 'Начнём с аккаунта' : 'С возвращением';
    $('auth-description').textContent = register ? 'Выберите роль и создайте своё пространство.' : 'Войдите, чтобы открыть каталог товаров.';
    $('auth-submit').querySelector('span').textContent = register ? 'Создать аккаунт' : 'Войти в Маркет';
    $('auth-password').minLength = register ? 8 : 1;
    $('auth-password').autocomplete = register ? 'new-password' : 'current-password';
    $('auth-password').placeholder = register ? 'Не меньше 8 символов' : 'Введите пароль';
    $('role-field').hidden = !register;
    setError('auth-error');
  }

  function showApp() {
    $('auth-screen').hidden = true;
    $('app-screen').hidden = false;
    $('account-name').textContent = state.session.email || 'Мой аккаунт';
    $('account-name').title = state.session.email || 'Мой аккаунт';
    $('account-avatar').textContent = (state.session.email || 'М').slice(0, 1).toUpperCase();
    $('account-role').textContent = isSeller() ? 'Продавец' : 'Покупатель';
    $('create-product').hidden = !isSeller();
    $('catalog-description').textContent = isSeller() ? 'Управляйте каталогом и находите место для нового.' : 'Знакомьтесь с ассортиментом и находите нужные вещи.';
    $('ownership-note').textContent = isSeller() ? 'Редактировать и архивировать можно свои товары. Каталог виден всем участникам.' : 'Вы смотрите общий каталог. Товары добавляют и обновляют продавцы.';
    document.title = 'Товары — Маркет';
    loadProducts();
    loadStats();
  }

  function logout(message = '') {
    state.authVersion++;
    state.listVersion++;
    state.statsVersion++;
    storeSession(null);
    state.items = [];
    state.page = 0;
    state.category = '';
    state.status = '';
    state.product = null;
    state.archive = null;
    $('category-filter').value = '';
    $('status-filter').value = '';
    $('products-region').replaceChildren();
    $('category-options').replaceChildren();
    document.querySelectorAll('dialog[open]').forEach((dialog) => dialog.close());
    $('app-screen').hidden = true;
    $('auth-screen').hidden = false;
    $('auth-password').value = '';
    setAuthMode(false);
    setError('auth-error', message);
    document.title = 'Вход — Маркет';
  }

  async function loadStats() {
    const version = ++state.statsVersion;
    const targets = [['stat-total', ''], ['stat-active', '&status=ACTIVE'], ['stat-archived', '&status=ARCHIVED']];
    targets.forEach(([id]) => { $(id).textContent = '—'; });
    $('nav-total').textContent = '—';
    await Promise.all(targets.map(async ([id, query]) => {
      try {
        const data = await api(`/products?page=0&size=1${query}`);
        if (version !== state.statsVersion) return;
        $(id).textContent = number.format(data.totalElements);
        if (id === 'stat-total') $('nav-total').textContent = number.format(data.totalElements);
      } catch { /* Listing has the actionable error; unavailable summary values remain empty. */ }
    }));
  }

  function updateNavigation() {
    $('catalog-nav').classList.toggle('current', state.status !== 'ARCHIVED');
    $('archive-nav').classList.toggle('current', state.status === 'ARCHIVED');
    $('reset-filters').hidden = !state.status && !state.category;
  }

  async function loadProducts() {
    const version = ++state.listVersion;
    state.loading = true;
    $('products-region').setAttribute('aria-busy', 'true');
    $('products-region').innerHTML = '<div class="loading-state"><span class="spinner" aria-hidden="true"></span>Загружаем товары…</div>';
    setError('catalog-error');
    updateNavigation();
    renderPagination();
    const query = new URLSearchParams({ page: state.page, size: state.size });
    if (state.status) query.set('status', state.status);
    if (state.category) query.set('category', state.category);
    try {
      const data = await api(`/products?${query}`);
      if (version !== state.listVersion) return;
      state.items = data.items || [];
      state.total = data.totalElements;
      if (state.page > 0 && !state.items.length) {
        state.page = Math.max(0, Math.ceil(state.total / state.size) - 1);
        return loadProducts();
      }
      state.loading = false;
      $('result-count').textContent = number.format(state.total);
      const categories = [...new Set(state.items.map((product) => product.category))].sort((a, b) => a.localeCompare(b, 'ru'));
      $('category-options').innerHTML = categories.map((value) => `<option value="${escapeHTML(value)}"></option>`).join('');
      renderProducts();
    } catch (error) {
      if (version !== state.listVersion) return;
      state.loading = false;
      state.items = [];
      state.total = 0;
      $('result-count').textContent = '—';
      setError('catalog-error', error.message);
      $('products-region').innerHTML = '<div class="empty-state"><h3>Каталог пока недоступен</h3><p>Попробуйте загрузить товары ещё раз.</p><button type="button" class="button button-secondary" data-action="retry">Повторить</button></div>';
    } finally {
      if (version === state.listVersion) {
        $('products-region').setAttribute('aria-busy', 'false');
        renderPagination();
      }
    }
  }

  function renderPagination() {
    const pages = Math.max(1, Math.ceil(state.total / state.size));
    $('page-number').textContent = `${state.page + 1} / ${pages}`;
    $('page-range').textContent = state.loading ? 'Загрузка…' : state.total ? `${number.format(state.page * state.size + 1)}–${number.format(Math.min((state.page + 1) * state.size, state.total))} из ${number.format(state.total)}` : 'Нет товаров';
    $('previous-page').disabled = state.loading || state.page === 0;
    $('next-page').disabled = state.loading || state.page + 1 >= pages;
    $('reload').disabled = state.loading;
  }

  function renderProducts() {
    if (!state.items.length) {
      const filtered = state.status || state.category;
      const description = filtered ? 'Попробуйте другую категорию или сбросьте фильтры.' : isSeller() ? 'Добавьте первый товар — и у вашей витрины появится начало.' : 'Продавцы ещё не добавили товары. Загляните сюда чуть позже.';
      const action = filtered ? '<button class="button button-secondary" type="button" data-action="reset">Сбросить фильтры</button>' : isSeller() ? '<button class="button button-primary" type="button" data-action="create">Добавить первый товар</button>' : '';
      $('products-region').innerHTML = `<div class="empty-state"><div class="empty-icon"><svg class="icon" aria-hidden="true"><use href="#icon-box"/></svg></div><h3>${filtered ? 'Таких товаров пока нет' : 'Здесь начинается ваш каталог'}</h3><p>${description}</p>${action}</div>`;
      return;
    }
    const rows = state.items.map((product, index) => {
      const [label, className] = statuses[product.status] || ['Неизвестен', ''];
      const id = escapeHTML(product.id);
      const editable = canEdit(product);
      const actions = editable ? `<button type="button" class="icon-button" data-action="edit" data-id="${id}" aria-label="Редактировать ${escapeHTML(product.name)}" title="Редактировать"><svg class="icon" aria-hidden="true"><use href="#icon-edit"/></svg></button>${product.status !== 'ARCHIVED' ? `<button type="button" class="icon-button" data-action="archive" data-id="${id}" aria-label="Архивировать ${escapeHTML(product.name)}" title="В архив"><svg class="icon" aria-hidden="true"><use href="#icon-archive"/></svg></button>` : ''}` : '<span class="read-only">Просмотр</span>';
      return `<tr><td class="name-cell"><div class="product-name-cell"><span class="product-avatar tone-${index % 4}"><svg class="icon" aria-hidden="true"><use href="#icon-box"/></svg></span><div><button type="button" class="product-name" data-action="view" data-id="${id}">${escapeHTML(product.name)}</button><span class="product-subtitle">${escapeHTML(product.category)}${editable ? ' · Ваш товар' : ''}</span></div></div></td><td class="product-category">${escapeHTML(product.category)}</td><td class="price-cell">${escapeHTML(money.format(product.price))}</td><td class="stock-cell ${product.stock === 0 ? 'no-stock' : ''}">${number.format(product.stock)} шт.</td><td class="status-cell"><span class="badge ${className}">${label}</span></td><td class="actions-cell"><div class="row-actions">${actions}</div></td></tr>`;
    }).join('');
    $('products-region').innerHTML = `<table class="product-table"><caption class="sr-only">Товары каталога</caption><thead><tr><th scope="col">Товар</th><th scope="col" class="category-column">Категория</th><th scope="col">Цена</th><th scope="col">Остаток</th><th scope="col">Статус</th><th scope="col"><span class="sr-only">Действия</span></th></tr></thead><tbody>${rows}</tbody></table>`;
  }

  function resetFilters() {
    state.page = 0;
    state.status = '';
    state.category = '';
    $('status-filter').value = '';
    $('category-filter').value = '';
    loadProducts();
  }

  function openProduct(mode, product = null) {
    state.productMode = mode;
    state.product = product;
    $('product-form').reset();
    setError('product-form-error');
    const viewing = mode === 'view';
    $('product-fields').disabled = viewing;
    $('product-dialog-title').textContent = mode === 'create' ? 'Новый товар' : viewing ? 'Карточка товара' : 'Редактировать товар';
    $('product-save').textContent = mode === 'create' ? 'Добавить товар' : 'Сохранить изменения';
    $('product-save').hidden = viewing;
    $('product-save').disabled = false;
    $('product-edit').hidden = !viewing || !canEdit(product);
    $('product-cancel').textContent = viewing ? 'Закрыть' : 'Отмена';
    $('product-status-field').hidden = !product;
    for (const field of ['name', 'category', 'price', 'stock', 'description', 'status']) {
      $(`product-${field}`).value = product?.[field] ?? (field === 'status' ? 'ACTIVE' : field === 'stock' ? '0' : '');
    }
    if (!$('product-dialog').open) $('product-dialog').showModal();
    if (!viewing) $('product-name').focus();
  }

  async function openExisting(id, mode) {
    const version = state.authVersion;
    try {
      const product = await api(`/products/${encodeURIComponent(id)}`);
      if (version !== state.authVersion) return;
      openProduct(mode, product);
    } catch (error) { if (version === state.authVersion) setError('catalog-error', error.message); }
  }

  function lockDialog(dialog, locked) {
    state.saving = locked;
    dialog.querySelectorAll('button').forEach((button) => { button.disabled = locked; });
  }

  $('login-tab').addEventListener('click', () => setAuthMode(false));
  $('register-tab').addEventListener('click', () => setAuthMode(true));
  $('auth-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    setError('auth-error');
    const body = { email: $('auth-email').value.trim(), password: $('auth-password').value };
    if (state.register) {
      body.role = $('auth-role').value;
      if (new TextEncoder().encode(body.password).length > 72) {
        setError('auth-error', 'Пароль слишком длинный. Используйте до 72 латинских или до 36 кириллических символов.');
        return;
      }
    }
    const register = state.register;
    const label = $('auth-submit').querySelector('span');
    $('auth-submit').disabled = true;
    $('login-tab').disabled = true;
    $('register-tab').disabled = true;
    label.textContent = register ? 'Создаём аккаунт…' : 'Входим…';
    try {
      const tokens = await request(register ? '/auth/register' : '/auth/login', { method: 'POST', body });
      state.authVersion++;
      storeSession({ ...tokens, email: body.email });
      $('auth-password').value = '';
      showApp();
    } catch (error) { setError('auth-error', error.message); }
    finally {
      $('auth-submit').disabled = false;
      $('login-tab').disabled = false;
      $('register-tab').disabled = false;
      label.textContent = register ? 'Создать аккаунт' : 'Войти в Маркет';
    }
  });

  $('logout').addEventListener('click', () => logout());
  $('reload').addEventListener('click', () => { loadProducts(); loadStats(); });
  $('filter-form').addEventListener('submit', (event) => {
    event.preventDefault();
    state.category = $('category-filter').value.trim();
    state.status = $('status-filter').value;
    state.page = 0;
    loadProducts();
  });
  $('status-filter').addEventListener('change', () => {
    state.status = $('status-filter').value;
    state.page = 0;
    loadProducts();
  });
  $('reset-filters').addEventListener('click', resetFilters);
  $('catalog-nav').addEventListener('click', resetFilters);
  $('archive-nav').addEventListener('click', () => {
    state.page = 0;
    state.status = 'ARCHIVED';
    state.category = '';
    $('status-filter').value = 'ARCHIVED';
    $('category-filter').value = '';
    loadProducts();
  });
  $('page-size').addEventListener('change', () => { state.size = Number($('page-size').value); state.page = 0; loadProducts(); });
  $('previous-page').addEventListener('click', () => { if (state.page > 0) { state.page--; loadProducts(); } });
  $('next-page').addEventListener('click', () => { if ((state.page + 1) * state.size < state.total) { state.page++; loadProducts(); } });
  $('create-product').addEventListener('click', () => openProduct('create'));
  $('product-edit').addEventListener('click', () => openProduct('edit', state.product));

  $('products-region').addEventListener('click', (event) => {
    const button = event.target.closest('button[data-action]');
    if (!button) return;
    const { action, id } = button.dataset;
    if (action === 'create') openProduct('create');
    if (action === 'reset') resetFilters();
    if (action === 'retry') { loadProducts(); loadStats(); }
    if (action === 'edit' || action === 'view') openExisting(id, action);
    if (action === 'archive') {
      state.archive = state.items.find((product) => product.id === id);
      if (!state.archive) return;
      $('archive-product-name').textContent = state.archive.name;
      setError('archive-error');
      $('archive-dialog').showModal();
    }
  });

  document.querySelectorAll('.close-dialog').forEach((button) => button.addEventListener('click', () => {
    if (!state.saving) button.closest('dialog').close();
  }));
  document.querySelectorAll('dialog').forEach((dialog) => dialog.addEventListener('cancel', (event) => {
    if (state.saving) event.preventDefault();
  }));

  $('product-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    if (state.saving || state.productMode === 'view') return;
    const body = {
      name: $('product-name').value.trim(), category: $('product-category').value.trim(),
      price: Number($('product-price').value), stock: Number($('product-stock').value),
      description: $('product-description').value.trim() || null,
    };
    if (!body.name || !body.category || !Number.isFinite(body.price) || body.price < 0.01 || !Number.isInteger(body.stock) || body.stock < 0) {
      setError('product-form-error', 'Укажите название, категорию, цену больше нуля и целое неотрицательное количество.');
      return;
    }
    const editing = state.productMode === 'edit';
    const id = state.product?.id;
    if (editing) body.status = $('product-status').value;
    setError('product-form-error');
    lockDialog($('product-dialog'), true);
    $('product-save').textContent = 'Сохраняем…';
    const version = state.authVersion;
    try {
      await api(editing ? `/products/${encodeURIComponent(id)}` : '/products', { method: editing ? 'PUT' : 'POST', body });
      if (version !== state.authVersion) return;
      $('product-dialog').close();
      if (!editing) resetFilters();
      else loadProducts();
      loadStats();
      notify(editing ? 'Изменения сохранены' : 'Товар добавлен в каталог');
    } catch (error) {
      if (version === state.authVersion) setError('product-form-error', error.message);
    } finally {
      lockDialog($('product-dialog'), false);
      $('product-save').textContent = editing ? 'Сохранить изменения' : 'Добавить товар';
    }
  });

  $('confirm-archive').addEventListener('click', async () => {
    if (!state.archive || state.saving) return;
    setError('archive-error');
    lockDialog($('archive-dialog'), true);
    $('confirm-archive').textContent = 'Переносим…';
    const version = state.authVersion;
    try {
      await api(`/products/${encodeURIComponent(state.archive.id)}`, { method: 'DELETE' });
      if (version !== state.authVersion) return;
      $('archive-dialog').close();
      notify('Товар перенесён в архив');
      loadProducts();
      loadStats();
    } catch (error) {
      if (version === state.authVersion) setError('archive-error', error.message);
    } finally {
      lockDialog($('archive-dialog'), false);
      $('confirm-archive').textContent = 'В архив';
    }
  });

  try {
    const stored = JSON.parse(sessionStorage.getItem(storageKey));
    if (stored?.access_token && stored?.refresh_token && claims(stored.access_token)?.sub) storeSession(stored);
  } catch { storeSession(null); }
  setAuthMode(false);
  if (state.session) showApp();
})();
