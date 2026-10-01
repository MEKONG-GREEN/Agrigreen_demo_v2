/** Logo tạm (Q-19): ô trắng bo góc, mầm cây xanh — thay bằng logo TTKNQG khi có. */
export function BrandMark({ size = 36 }: { size?: number }) {
  return (
    <span className="brand-mark" style={{ width: size, height: size }} aria-hidden="true">
      <svg width={size * 0.62} height={size * 0.62} viewBox="0 0 24 24" fill="none" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 21v-9" stroke="#123B24" strokeWidth="2" />
        <path d="M12 12c0-3.5-2.6-6.5-7-6.5 0 4.2 2.8 6.5 7 6.5z" fill="#00A82D" stroke="#007A20" strokeWidth="1.4" />
        <path d="M12 12c0-3 2.4-5.8 7-5.8 0 3.9-2.7 5.8-7 5.8z" fill="#7CE495" stroke="#007A20" strokeWidth="1.4" />
        <path d="M7 21h10" stroke="#00A3E0" strokeWidth="2" />
      </svg>
    </span>
  );
}
