\NeedsTeXFormat{LaTeX2e}
\ProvidesClass{thesis}

\DeclareOption*{\PassOptionsToClass{\CurrentOption}{memoir}}
\ProcessOptions\relax
\LoadClass[12pt,a4paper,oneside]{memoir}

% --- Encoding & Fonts ---
\RequirePackage[utf8]{vntex,inputenc}
\RequirePackage[T5]{fontenc}
\RequirePackage{anyfontsize,mathptmx}
\RequirePackage[scaled=0.85]{beramono}  % Better monospace font for \texttt

% --- Core packages ---
\RequirePackage{longtable}
\RequirePackage{float}                 % provides [H] strict float placement
\RequirePackage[subsection]{placeins}  % auto \FloatBarrier at each \section AND \subsection
\RequirePackage{geometry,graphicx,indentfirst,setspace}
\RequirePackage{tikz}
\RequirePackage[center]{caption}
\RequirePackage{amsmath,amssymb}
\RequirePackage{enumitem}  % Better list control
\RequirePackage[table]{xcolor}

% --- Theme colors (used in headings, tables, etc.) ---
\definecolor{headerblue}{RGB}{41, 65, 122}
\definecolor{rowgray}{RGB}{240, 243, 250}

% --- Table styling: alternating rows + colored header ---
\RequirePackage{etoolbox}
\newcommand{\thead}[1]{\bfseries\color{white}#1}
\AtBeginEnvironment{table}{\rowcolors{2}{white}{rowgray}}

% --- Bibliography ---
\RequirePackage[super,square,sort]{natbib}

% --- Vietnamese labels ---
\RequirePackage[vietnamese]{babel}

% --- Hyperref MUST be near-last ---
\RequirePackage{hyperref}

% --- fancyhdr after hyperref ---
\RequirePackage{fancyhdr}

% ============================================================
% Custom metadata commands
% ============================================================
\newcommand{\Month}{%
  \ifcase \the\month
  \or Tháng 1\or Tháng 2\or Tháng 3\or Tháng 4%
  \or Tháng 5\or Tháng 6\or Tháng 7\or Tháng 8%
  \or Tháng 9\or Tháng 10\or Tháng 11\or Tháng 12%
  \fi
}

\newcommand{\@upperuniname}{BỘ KHOA HỌC VÀ CÔNG NGHỆ}
\newcommand{\@uniname}{HỌC VIỆN CÔNG NGHỆ BƯU CHÍNH VIỄN THÔNG}

\newcommand{\@deptname}{-------------------------------------}
\newcommand{\deptname}[1]{\renewcommand{\@deptname}{#1}}

\newcommand{\@reporttype}{Báo cáo đồ án}
\newcommand{\reporttype}[1]{\renewcommand{\@reporttype}{#1}}

\renewcommand{\@title}{Tên đề tài}

\newcommand{\@defcouncil}{Môn học}
\newcommand{\defcouncil}[1]{\renewcommand{\@defcouncil}{#1}}

\newcommand{\@supervisor}{Giảng viên}
\newcommand{\supervisor}[1]{\renewcommand{\@supervisor}{#1}}

\newcommand{\@groupnum}{Nhóm}
\newcommand{\groupnum}[1]{\renewcommand{\@groupnum}{#1}}

\newcommand{\@stuname}{Sinh viên}
\newcommand{\stuname}[1]{\renewcommand{\@stuname}{#1}}

\newcommand{\@reportplace}{Hà Nội}
\newcommand{\reportplace}[1]{\renewcommand{\@reportplace}{#1}}

\newcommand{\@reporttime}{\Month{} năm \the\year}
\newcommand{\reporttime}[1]{\renewcommand{\@reporttime}{#1}}

% ============================================================
% Page geometry & typography
% ============================================================
\usetikzlibrary{calc, shapes.geometric, arrows.meta, positioning, fit, backgrounds, decorations.pathreplacing}
\RequirePackage[underline=false]{pgf-umlsd}

\geometry{
  paper=a4paper,
  left=3cm,
  right=2cm,
  vmargin=2cm,
  includeheadfoot=true,
  headheight=30pt
}

% Fix: proper leading (1.2x font size) instead of 1:1
\renewcommand{\large}{\fontsize{15}{18}\selectfont}
\renewcommand{\Large}{\fontsize{20}{24}\selectfont}

\OnehalfSpacing
\raggedbottom
\renewcommand{\arraystretch}{1.2}

% Numbering depth
\setsecnumdepth{subsection}
\settocdepth{subsection}

% TOC numwidths
\setlength{\cfttablenumwidth}{3em}
\setlength{\cftfigurenumwidth}{3em}

% ============================================================
% Vietnamese labels for all document elements
% ============================================================
\AtBeginDocument{
  \renewcommand*\bibname{Tài liệu tham khảo}
  \renewcommand{\contentsname}{Mục lục}
  \renewcommand{\listtablename}{Danh sách bảng}
  \renewcommand{\listfigurename}{Danh sách hình}
  \renewcommand{\chaptername}{Chương}
  \renewcommand{\appendixname}{Phụ lục}
  \renewcommand{\tablename}{Bảng}
  \renewcommand{\figurename}{Hình}
}

% ============================================================
% Hyperref setup
% ============================================================
\hypersetup{
  colorlinks=true,
  linkcolor=black,
  citecolor=blue!60!black,
  urlcolor=blue!70!black,
  bookmarksnumbered=true,
  bookmarksopen=true,
  pdfstartview=FitH
}

% ============================================================
% Header / Footer
% ============================================================
\renewcommand{\headrulewidth}{0.3pt}
\renewcommand{\footrulewidth}{0pt}

\pagestyle{fancy}
\fancyhf{}
\fancyhead[R]{\bfseries\nouppercase\rightmark}
\fancyfoot[C]{\thepage}

% ============================================================
% Cover page
% ============================================================
\newcommand{\coverpage}{
  \pagestyle{empty}
  \large\bfseries

  \begin{tikzpicture}[remember picture,overlay]
    \draw[line width=3pt](
    $(current page.north west) + (3.0cm,-2.0cm)$
    ) rectangle (
    $(current page.south east) + (-2.0cm,2.0cm)$
    );
    \draw[line width=1pt](
    $(current page.north west) + (2.9cm,-1.9cm)$
    ) rectangle (
    $(current page.south east) + (-1.9cm,1.9cm)$
    );
  \end{tikzpicture}

  \vspace*{-3\baselineskip}
  \begin{center}
    \MakeUppercase{\@upperuniname}

    \MakeUppercase{\@uniname}

    \MakeUppercase{\@deptname}
    \vfill
    \includegraphics[scale=0.6]{graphics/ptit.png}
    \vfill
    \MakeUppercase{\@reporttype}
    \vfill
    {\Large\MakeUppercase{\parbox[c]{0.8\linewidth}{\centering\@title}}}
  \end{center}
  \vfill
  \hfill\parbox[t]{13cm}{
    Môn học \@defcouncil

    Giảng viên hướng dẫn: \parbox[t]{7cm}{\@supervisor}

    Thực hiện bởi nhóm \parbox[t]{7cm}{\@groupnum}

    \@stuname
  }
  \vfill
  \mdseries
  \begin{center}
    \MakeUppercase{\@reportplace, \@reporttime}
  \end{center}
  \normalsize
  \clearpage%

  \pagestyle{fancy}
}

% ============================================================
% Preface environment (for front matter sections)
% ============================================================
\providecommand{\phantomsection}{}
\newenvironment{preface}[1]{%
  \clearpage%
  \thispagestyle{plain}
  {\noindent \bfseries \Huge #1}%

  \vspace{1em}
  \par\noindent
}{%
  \clearpage%
}

% ============================================================
% List styling — tighter spacing
% ============================================================
\setlist{nosep, leftmargin=2em}
\setlist[itemize,1]{label=\textbullet}
\setlist[itemize,2]{label=--}
\setlist[enumerate,1]{label=\arabic*.}

% ============================================================
% Chapter heading style — cleaner look
% ============================================================
\setlength{\beforechapskip}{0pt}
\renewcommand{\chapterheadstart}{\vspace*{-\topskip}\vspace*{-\baselineskip}\vspace*{-\parskip}}
\renewcommand{\afterchapternum}{\par\nobreak\vskip 8pt}
\renewcommand{\printchaptername}{}
\renewcommand{\chapternamenum}{}
\renewcommand{\printchapternum}{%
  \begin{center}
    \color{headerblue}%
    \rule{\linewidth}{1.5pt}\\[6pt]
    {\fontsize{13}{15}\selectfont\MakeUppercase{\chaptername}~\thechapter}
  \end{center}
}
\renewcommand{\printchaptertitle}[1]{%
  \begin{center}
    {\Large\bfseries\MakeUppercase{#1}}\\[4pt]
    \color{headerblue}\rule{\linewidth}{1.5pt}
  \end{center}
}
\renewcommand{\afterchaptertitle}{\par\nobreak\vskip 15pt}

% ============================================================
% Section heading style — subtle color accent
% ============================================================
\setsecheadstyle{\large\bfseries\color{headerblue!90!black}}
\setsubsecheadstyle{\normalsize\bfseries}

% ============================================================
% Table of contents styling
% ============================================================
\renewcommand{\cftchapterfont}{\bfseries}
\renewcommand{\cftchapterpagefont}{\bfseries}
\setlength{\cftbeforechapterskip}{6pt}
\setlength{\cftbeforesectionskip}{2pt}
