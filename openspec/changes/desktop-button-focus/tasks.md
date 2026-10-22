## 1. Desktop

- [x] 1.1 `volButton` implements `fyne.Focusable`: the focus ring (`newRingRenderer`), Space/Return/Enter press it,
      a disabled one is neither focused by Tab nor pressed
- [x] 1.2 `TestFocusRing` covers a button; `TestButtonKeyboard`: Tab skips a disabled button, Space/Return/Enter
      press, the ring leaves with the focus

## 2. Docs and validation

- [x] 2.1 `docs/themes.md` and `four-new-themes` (accepted differences) updated
- [x] 2.2 `make check && make test && make build`
