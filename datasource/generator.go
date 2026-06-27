package datasource

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/example/rule-engine-demo/model"
)

// Domain value pools used by both the user generator and the rule generator so
// that generated rules actually hit a realistic fraction of generated users.
var (
	provinces = []string{"广东", "江苏", "浙江", "上海", "北京", "四川", "山东", "湖北", "福建", "湖南"}
	cities    = map[string][]string{
		"广东": {"深圳", "广州", "东莞"}, "江苏": {"南京", "苏州", "无锡"}, "浙江": {"杭州", "宁波", "温州"},
		"上海": {"上海"}, "北京": {"北京"}, "四川": {"成都", "绵阳"}, "山东": {"济南", "青岛"},
		"湖北": {"武汉", "宜昌"}, "福建": {"福州", "厦门"}, "湖南": {"长沙", "株洲"},
	}
	genders        = []string{"男", "女"}
	occupations    = []string{"程序员", "教师", "销售", "学生", "产品经理", "设计师", "医生", "律师", "自由职业", "公务员"}
	educations     = []string{"高中", "大专", "本科", "硕士", "博士"}
	maritalStatus  = []string{"未婚", "已婚", "离异"}
	incomeLevels   = []string{"<5k", "5k-10k", "10k-20k", "15k-20k", "20k-30k", "30k+"}
	categories     = []string{"数码", "数码产品", "图书", "汽车", "美妆", "家电", "服饰", "食品", "母婴", "运动"}
	deviceTypes    = []string{"iPhone", "Android", "iPad", "Windows", "Mac"}
	osTypes        = []string{"iOS", "Android", "Windows", "macOS"}
	browsers       = []string{"Safari", "Chrome", "Edge", "Firefox"}
	appVersions    = []string{"8.0.0", "8.1.5", "8.2.0", "8.2.1", "9.0.0"}
	riskLevels     = []string{"低", "中", "高"}
	tagPool        = []string{"高价值", "数码爱好者", "夜猫子", "亲子", "教育", "高频登录", "商务", "忠诚用户", "学生", "价格敏感", "会员", "智能家居", "高净值", "高消费"}
)

// Generator produces deterministic synthetic users and rules from a seed.
type Generator struct {
	rng *rand.Rand
}

// NewGenerator returns a generator seeded for reproducible output.
func NewGenerator(seed int64) *Generator {
	return &Generator{rng: rand.New(rand.NewSource(seed))}
}

// Users generates n wide-table users with realistic, correlated fields. Each
// user's columns are stored directly in a Fields map (no reflection needed at
// match time). Numeric fields use native int/float64 types.
func (g *Generator) Users(n int) []model.User {
	users := make([]model.User, n)
	for i := 0; i < n; i++ {
		prov := pick(g.rng, provinces)
		city := pick(g.rng, cities[prov])
		orderCount := g.rng.Intn(400)
		avg := 50 + g.rng.Float64()*500
		total := float64(orderCount) * avg
		uid := int64(i + 1)
		fields := map[string]any{
			"uid":               uid,
			"username":          fmt.Sprintf("user%06d", i+1),
			"gender":            pick(g.rng, genders),
			"age":               18 + g.rng.Intn(50),
			"province":          prov,
			"city":              city,
			"occupation":        pick(g.rng, occupations),
			"education":         pick(g.rng, educations),
			"marital_status":    pick(g.rng, maritalStatus),
			"income_level":      pick(g.rng, incomeLevels),
			"vip_level":         g.rng.Intn(6),
			"register_days":     g.rng.Intn(2000),
			"login_days_30d":    g.rng.Intn(31),
			"order_count":       orderCount,
			"total_amount":      round2(total),
			"avg_order_amount":  round2(avg),
			"favorite_category": pick(g.rng, categories),
			"device_type":       pick(g.rng, deviceTypes),
			"os_type":           pick(g.rng, osTypes),
			"browser":           pick(g.rng, browsers),
			"app_version":       pick(g.rng, appVersions),
			"active_score":      round2(g.rng.Float64() * 100),
			"credit_score":      400 + g.rng.Intn(450),
			"risk_level":        pick(g.rng, riskLevels),
			"tag1":              pick(g.rng, tagPool),
			"tag2":              pick(g.rng, tagPool),
			"tag3":              pick(g.rng, tagPool),
		}
		users[i] = model.User{UID: uid, Fields: fields}
	}
	return users
}

// Rules generates n rules, each a conjunction (AND) of 2–5 random predicates
// drawn from a template set. Every generated expression is valid qlbridge SQL
// and exercises =, >=, <, BETWEEN, IN and LIKE.
func (g *Generator) Rules(n int) []model.Rule {
	rules := make([]model.Rule, n)
	for i := 0; i < n; i++ {
		nPred := 2 + g.rng.Intn(4) // 2..5 predicates
		preds := make([]string, 0, nPred)
		seen := map[string]bool{}
		for len(preds) < nPred {
			p, key := g.predicate()
			if seen[key] {
				continue
			}
			seen[key] = true
			preds = append(preds, p)
		}
		rules[i] = model.Rule{
			ID:       int64(10000 + i),
			Name:     fmt.Sprintf("rule_%05d", i),
			Expr:     strings.Join(preds, " AND "),
			Priority: g.rng.Intn(20),
			Enabled:  true,
		}
	}
	return rules
}

// predicate returns a single random predicate plus a key identifying its field
// (so a rule does not contain two predicates on the same field).
func (g *Generator) predicate() (expr string, key string) {
	switch g.rng.Intn(11) {
	case 0:
		lo := 18 + g.rng.Intn(30)
		hi := lo + 5 + g.rng.Intn(20)
		return fmt.Sprintf("age BETWEEN %d AND %d", lo, hi), "age"
	case 1:
		return fmt.Sprintf("province IN (%s)", inList(pickN(g.rng, provinces, 1+g.rng.Intn(3)))), "province"
	case 2:
		return fmt.Sprintf("income_level IN (%s)", inList(pickN(g.rng, incomeLevels, 1+g.rng.Intn(3)))), "income_level"
	case 3:
		return "favorite_category LIKE '数%'", "favorite_category"
	case 4:
		return fmt.Sprintf("active_score >= %d", 50+g.rng.Intn(50)), "active_score"
	case 5:
		return fmt.Sprintf("vip_level >= %d", 1+g.rng.Intn(4)), "vip_level"
	case 6:
		return fmt.Sprintf("gender = '%s'", pick(g.rng, genders)), "gender"
	case 7:
		return fmt.Sprintf("total_amount > %d", 1000*g.rng.Intn(60)), "total_amount"
	case 8:
		return fmt.Sprintf("login_days_30d >= %d", g.rng.Intn(25)), "login_days_30d"
	case 9:
		return fmt.Sprintf("risk_level = '%s'", pick(g.rng, riskLevels)), "risk_level"
	default:
		return fmt.Sprintf("credit_score < %d", 500+g.rng.Intn(350)), "credit_score"
	}
}

// --- helpers ---

func pick[T any](r *rand.Rand, xs []T) T { return xs[r.Intn(len(xs))] }

// pickN returns up to k distinct elements from xs.
func pickN(r *rand.Rand, xs []string, k int) []string {
	if k >= len(xs) {
		out := make([]string, len(xs))
		copy(out, xs)
		return out
	}
	idx := r.Perm(len(xs))[:k]
	out := make([]string, k)
	for i, j := range idx {
		out[i] = xs[j]
	}
	return out
}

// inList renders []string{"a","b"} as "'a','b'" for SQL IN clauses.
func inList(xs []string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = "'" + x + "'"
	}
	return strings.Join(parts, ",")
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
