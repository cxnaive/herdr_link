图最下面的 \(p(A\mid \mathbf x)\) 是用贝叶斯公式得到的。关键是：给定 A 看观测，再给定观测反推 A。
这里 \(\mathbf x=(x[0],\ldots,x[N-1])\) 表示全部观测。

### ① 先看似然

因为 \(x[n]=A+w[n]\)，而 \(w[n]\sim\mathcal N(0,\sigma^2)\)，所以 \(x[n]\mid A\sim\mathcal N(A,\sigma^2)\)。各次噪声独立，因此把每个观测的密度相乘：

\[
p(\mathbf x\mid A)
=\prod_{n=0}^{N-1}\frac{1}{\sqrt{2\pi\sigma^2}}
\exp\!\left[-\frac{(x[n]-A)^2}{2\sigma^2}\right]
\propto
\exp\!\left[-\frac{1}{2\sigma^2}\sum_{n=0}^{N-1}(x[n]-A)^2\right].
\]

这就解释了求和：多个指数相乘，指数里的项就相加。

### ② 再看先验

题目给了 \(A\sim\mathcal N(\mu_A,\sigma_A^2)\)，所以

\[
p(A)\propto \exp\!\left[-\frac{(A-\mu_A)^2}{2\sigma_A^2}\right].
\]

### ③ 用贝叶斯公式

观测 \(\mathbf x\) 已经固定，分母 \(p(\mathbf x)\) 不随 A 变化，所以

\[
p(A\mid\mathbf x)
=\frac{p(\mathbf x\mid A)\,p(A)}{p(\mathbf x)}
\propto
\exp\!\left[
\underbrace{-\frac{1}{2\sigma^2}\sum_{n=0}^{N-1}(x[n]-A)^2}_{\text{来自观测：}A\text{ 与数据有多接近}}
\underbrace{-\frac{(A-\mu_A)^2}{2\sigma_A^2}}_{\text{来自先验：}A\text{ 与先验均值有多接近}}
\right].
\]
