// Access token hiện tại — CHỈ trong bộ nhớ của tab (không localStorage/sessionStorage/cookie đọc được bằng JS
// ngoài tab). Tải lại trang thì mất và được khôi phục bằng đăng nhập im lặng qua phiên SSO Keycloak.
let accessToken: string | null = null;

export const tokenStore = {
  get: (): string | null => accessToken,
  set: (t: string | null): void => {
    accessToken = t;
  },
};
