(defun yapf-format ()
 (interactive)
 (let ((initial-location (point)))
   (shell-command-on-region 1 (+ (buffer-size) 1) "yapf" t t)
   (goto-char initial-location)
 ))

; TODO consider putting this only in python mode
(global-set-key [?\C-x ?\C-a] 'yapf-format)

(defun add-yapf-save-hook ()
  (add-hook 'before-save-hook 'yapf-format nil t))

; Some hooks for yapf
; (add-hook 'python-mode-hook 'add-yapf-save-hook)
; (remove-hook 'before-save-hook 'yapf-format)


; Production database client

(defun show-stuck-builds()
  (interactive)
  (term-send-raw-string "select build_code, server_name, status from build_requests where now() - last_updated >= '2 hours'::INTERVAL and status > 0 and status < 10;\n"))

(defun show-active-builds()
  (interactive)
  (term-send-raw-string "select build_code, server_name, status from build_requests where status > 0 and status < 10;\n"))

(defun fail-build()
  (interactive)
  (term-send-raw-string "UPDATE build_requests SET status=-2 WHERE build_code='';"))

(defun db-client ()
  (interactive)
  (term "bash")
  (rename-buffer "production-db")
  (term-send-raw-string "psql $(pass dev/teams/highland/production/db_url)\n")
  (define-key term-raw-map "\C-f" 'show-stuck-builds)
  (define-key term-raw-map "\C-u" 'fail-build)
  (define-key term-raw-map "\C-b" 'show-active-builds)
  )


; TODO write docstring folder