// Copyright ©2016 The Gonum Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package distmv

import (
	"math"
	"math/rand/v2"
	"sort"

	"golang.org/x/tools/container/intsets"

	"gonum.org/v1/gonum/floats"
	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat"
	"gonum.org/v1/gonum/stat/distuv"
)

// StudentsT is a multivariate Student's T distribution. It is a distribution over
// ℝ^n with the probability density
//
//	p(y) = (Γ((ν+n)/2) / Γ(ν/2)) * (νπ)^(-n/2) * |Ʃ|^(-1/2) *
//	           (1 + 1/ν * (y-μ)ᵀ * Ʃ^-1 * (y-μ))^(-(ν+n)/2)
//
// where ν is a scalar greater than 2, μ is a vector in ℝ^n, and Ʃ is an n×n
// symmetric positive definite matrix.
//
// In this distribution, ν sets the spread of the distribution, similar to
// the degrees of freedom in a univariate Student's T distribution. As ν → ∞,
// the distribution approaches a multi-variate normal distribution.
// μ is the mean of the distribution, and the covariance is  ν/(ν-2)*Ʃ.
//
// See https://en.wikipedia.org/wiki/Student%27s_t-distribution and
// http://users.isy.liu.se/en/rt/roth/student.pdf for more information.
type StudentsT struct {
	nu float64
	mu []float64
	// If src is altered, rnd must be updated.
	src rand.Source
	rnd *rand.Rand

	sigma mat.SymDense // only stored if needed

	chol       mat.Cholesky
	lower      mat.TriDense
	logSqrtDet float64
	dim        int
}

// NewStudentsT creates a new StudentsT with the given nu, mu, and sigma
// parameters.
//
// NewStudentsT panics if len(mu) == 0, or if len(mu) != sigma.SymmetricDim(). If
// the covariance matrix is not positive-definite, nil is returned and ok is false.
func NewStudentsT(mu []float64, sigma mat.Symmetric, nu float64, src rand.Source) (dist *StudentsT, ok bool) {
	if len(mu) == 0 {
		panic(badZeroDimension)
	}
	dim := sigma.SymmetricDim()
	if dim != len(mu) {
		panic(badSizeMismatch)
	}

	s := &StudentsT{
		nu:  nu,
		mu:  make([]float64, dim),
		dim: dim,
		src: src,
	}
	if src != nil {
		s.rnd = rand.New(src)
	}
	copy(s.mu, mu)

	ok = s.chol.Factorize(sigma)
	if !ok {
		return nil, false
	}
	s.sigma = *mat.NewSymDense(dim, nil)
	s.sigma.CopySym(sigma)
	s.chol.LTo(&s.lower)
	s.logSqrtDet = 0.5 * s.chol.LogDet()
	return s, true
}

// ConditionStudentsT returns the Student's T distribution that is the receiver
// conditioned on the input evidence, and the success of the operation.
// The returned Student's T has dimension
// n - len(observed), where n is the dimension of the original receiver.
// The dimension order is preserved during conditioning, so if the value
// of dimension 1 is observed, the returned normal represents dimensions {0, 2, ...}
// of the original Student's T distribution.
//
// ok indicates whether there was a failure during the update. If ok is false
// the operation failed and dist is not usable.
// Mathematically this is impossible, but can occur with finite precision arithmetic.
func (s *StudentsT) ConditionStudentsT(observed []int, values []float64, src rand.Source) (dist *StudentsT, ok bool) {
	if len(observed) == 0 {
		panic("studentst: no observed value")
	}
	if len(observed) != len(values) {
		panic(badInputLength)
	}

	for _, v := range observed {
		if v < 0 || v >= s.dim {
			panic("studentst: observed value out of bounds")
		}
	}

	newNu, newMean, newSigma := studentsTConditional(observed, values, s.nu, s.mu, &s.sigma)
	if newMean == nil {
		return nil, false
	}

	return NewStudentsT(newMean, newSigma, newNu, src)

}

// studentsTConditional updates a Student's T distribution based on the observed samples
// (see documentation for the public function). The Gaussian conditional update
// is treated as a special case when  nu == math.Inf(1).
func studentsTConditional(observed []int, values []float64, nu float64, mu []float64, sigma mat.Symmetric) (newNu float64, newMean []float64, newSigma *mat.SymDense) {
	dim := len(mu)
	ob := len(observed)

	unobserved := findUnob(observed, dim)

	unob := len(unobserved)
	if unob == 0 {
		panic("stat: all dimensions observed")
	}

	mu1 := make([]float64, unob)
	for i, v := range unobserved {
		mu1[i] = mu[v]
	}
	mu2 := make([]float64, ob) // really v - mu2
	for i, v := range observed {
		mu2[i] = values[i] - mu[v]
	}

	var sigma11, sigma22 mat.SymDense
	sigma11.SubsetSym(sigma, unobserved)
	sigma22.SubsetSym(sigma, observed)

	sigma21 := mat.NewDense(ob, unob, nil)
	for i, r := range observed {
		for j, c := range unobserved {
			v := sigma.At(r, c)
			sigma21.Set(i, j, v)
		}
	}

	var chol mat.Cholesky
	ok := chol.Factorize(&sigma22)
	if !ok {
		return math.NaN(), nil, nil
	}

	// Compute mu_1 + sigma_{2,1}ᵀ * sigma_{2,2}^-1 (v - mu_2).
	v := mat.NewVecDense(ob, mu2)
	var tmp, tmp2 mat.VecDense
	err := chol.SolveVecTo(&tmp, v)
	if err != nil {
		return math.NaN(), nil, nil
	}
	tmp2.MulVec(sigma21.T(), &tmp)

	for i := range mu1 {
		mu1[i] += tmp2.At(i, 0)
	}

	// Compute tmp4 = sigma_{2,1}ᵀ * sigma_{2,2}^-1 * sigma_{2,1}.
	// TODO(btracey): Should this be a method of SymDense?
	var tmp3, tmp4 mat.Dense
	err = chol.SolveTo(&tmp3, sigma21)
	if err != nil {
		return math.NaN(), nil, nil
	}
	tmp4.Mul(sigma21.T(), &tmp3)

	// Compute sigma_{1,1} - tmp4
	// TODO(btracey): If tmp4 can constructed with a method, then this can be
	// replaced with SubSym.
	for i := 0; i < len(unobserved); i++ {
		for j := i; j < len(unobserved); j++ {
			v := sigma11.At(i, j)
			sigma11.SetSym(i, j, v-tmp4.At(i, j))
		}
	}

	// The computed variables are accurate for a Normal.
	if math.IsInf(nu, 1) {
		return nu, mu1, &sigma11
	}

	// Compute beta = (v - mu_2)ᵀ * sigma_{2,2}^-1 * (v - mu_2)ᵀ
	beta := mat.Dot(v, &tmp)

	// Scale the covariance matrix
	sigma11.ScaleSym((nu+beta)/(nu+float64(ob)), &sigma11)

	return nu + float64(ob), mu1, &sigma11
}

// findUnob returns the unobserved variables (the complementary set to observed).
// findUnob panics if any value repeated in observed.
func findUnob(observed []int, dim int) (unobserved []int) {
	var setOb intsets.Sparse
	for _, v := range observed {
		setOb.Insert(v)
	}
	var setAll intsets.Sparse
	for i := 0; i < dim; i++ {
		setAll.Insert(i)
	}
	var setUnob intsets.Sparse
	setUnob.Difference(&setAll, &setOb)
	unobserved = setUnob.AppendTo(nil)
	sort.Ints(unobserved)
	return unobserved
}

// CovarianceMatrix calculates the covariance matrix of the distribution,
// storing the result in dst. Upon return, the value at element {i, j} of the
// covariance matrix is equal to the covariance of the i^th and j^th variables.
//
//	covariance(i, j) = E[(x_i - E[x_i])(x_j - E[x_j])]
//
// If the dst matrix is empty it will be resized to the correct dimensions,
// otherwise dst must match the dimension of the receiver or CovarianceMatrix
// will panic.
func (st *StudentsT) CovarianceMatrix(dst *mat.SymDense) {
	if dst.IsEmpty() {
		*dst = *(dst.GrowSym(st.dim).(*mat.SymDense))
	} else if dst.SymmetricDim() != st.dim {
		panic("studentst: input matrix size mismatch")
	}
	dst.CopySym(&st.sigma)
	dst.ScaleSym(st.nu/(st.nu-2), dst)
}

// Dim returns the dimension of the distribution.
func (s *StudentsT) Dim() int {
	return s.dim
}

// LogProb computes the log of the pdf of the point x.
func (s *StudentsT) LogProb(y []float64) float64 {
	if len(y) != s.dim {
		panic(badInputLength)
	}

	nu := s.nu
	n := float64(s.dim)
	lg1, _ := math.Lgamma((nu + n) / 2)
	lg2, _ := math.Lgamma(nu / 2)

	t1 := lg1 - lg2 - n/2*math.Log(nu*math.Pi) - s.logSqrtDet

	mahal := stat.Mahalanobis(mat.NewVecDense(len(y), y), mat.NewVecDense(len(s.mu), s.mu), &s.chol)
	mahal *= mahal
	return t1 - ((nu+n)/2)*math.Log(1+mahal/nu)
}

// MarginalStudentsT returns the marginal distribution of the given input variables,
// and the success of the operation.
// That is, MarginalStudentsT returns
//
//	p(x_i) = \int_{x_o} p(x_i | x_o) p(x_o) dx_o
//
// where x_i are the dimensions in the input, and x_o are the remaining dimensions.
// See https://en.wikipedia.org/wiki/Marginal_distribution for more information.
//
// The input src is passed to the created StudentsT.
//
// ok indicates whether there was a failure during the marginalization. If ok is false
// the operation failed and dist is not usable.
// Mathematically this is impossible, but can occur with finite precision arithmetic.
func (s *StudentsT) MarginalStudentsT(vars []int, src rand.Source) (dist *StudentsT, ok bool) {
	newMean := make([]float64, len(vars))
	for i, v := range vars {
		newMean[i] = s.mu[v]
	}
	var newSigma mat.SymDense
	newSigma.SubsetSym(&s.sigma, vars)
	return NewStudentsT(newMean, &newSigma, s.nu, src)
}

// MarginalStudentsTSingle returns the marginal distribution of the given input variable.
// That is, MarginalStudentsTSingle returns
//
//	p(x_i) = \int_{x_o} p(x_i | x_o) p(x_o) dx_o
//
// where i is the input index, and x_o are the remaining dimensions.
// See https://en.wikipedia.org/wiki/Marginal_distribution for more information.
//
// The input src is passed to the call to NewStudentsT.
func (s *StudentsT) MarginalStudentsTSingle(i int, src rand.Source) distuv.StudentsT {
	return distuv.StudentsT{
		Mu:    s.mu[i],
		Sigma: math.Sqrt(s.sigma.At(i, i)),
		Nu:    s.nu,
		Src:   src,
	}
}

// TODO(btracey): Implement marginal single. Need to modify univariate StudentsT
// to be three-parameter.

// Mean returns the mean of the probability distribution.
//
// If dst is not nil, the mean will be stored in-place into dst and returned,
// otherwise a new slice will be allocated first. If dst is not nil, it must
// have length equal to the dimension of the distribution.
func (s *StudentsT) Mean(dst []float64) []float64 {
	dst = reuseAs(dst, s.dim)
	copy(dst, s.mu)
	return dst
}

// Nu returns the degrees of freedom parameter of the distribution.
func (s *StudentsT) Nu() float64 {
	return s.nu
}

// Prob computes the value of the probability density function at x.
func (s *StudentsT) Prob(y []float64) float64 {
	return math.Exp(s.LogProb(y))
}

// Rand generates a random sample according to the distribution.
//
// If dst is not nil, the sample will be stored in-place into dst and returned,
// otherwise a new slice will be allocated first. If dst is not nil, it must
// have length equal to the dimension of the distribution.
func (s *StudentsT) Rand(dst []float64) []float64 {
	// If Y is distributed according to N(0,Sigma), and U is chi^2 with
	// parameter ν, then
	//  X = mu + Y * sqrt(nu / U)
	// X is distributed according to this distribution.

	// Generate Y.
	dst = reuseAs(dst, s.dim)
	if s.rnd == nil {
		for i := range dst {
			dst[i] = rand.NormFloat64()
		}
	} else {
		for i := range dst {
			dst[i] = s.rnd.NormFloat64()
		}
	}
	y := mat.NewVecDense(s.dim, dst)
	y.MulVec(&s.lower, y)
	// Compute mu + Y*sqrt(nu/U)
	u := distuv.ChiSquared{K: s.nu, Src: s.src}.Rand()
	floats.AddScaledTo(dst, s.mu, math.Sqrt(s.nu/u), dst)
	return dst
}
