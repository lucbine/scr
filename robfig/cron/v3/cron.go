package cron

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Cron keeps track of any number of entries, invoking the associated func as
// specified by the schedule. It may be started, stopped, and the entries may
// be inspected while running.
type Cron struct {
	entries []*Entry //job 执行的实体
	// chain 用来定义entry里的warppedJob使用什么逻辑（e.g. skipIfLastRunning）
	// 即一个cron里所有entry只有一个封装逻辑
	chain     Chain
	stop      chan struct{}     //停止整个cron 信号
	add       chan *Entry       //添加一个entry
	remove    chan EntryID      //删除一个entry
	snapshot  chan chan []Entry //获取整个entry快照 (利用chan嵌套chan 实现函数异步执行 顺序返回值 :https://segmentfault.com/a/1190000018475209)
	running   bool              //代表是否已经在执行，是cron为使用者提供的动态修改entry的接口准备的
	logger    Logger            //封装golang的log包
	runningMu sync.Mutex        //用来修改运行中的cron数据，比如增加entry，移除entry
	location  *time.Location    //时区
	parser    ScheduleParser    //对时间格式的解析，为interface, 可以定制自己的时间规则
	nextID    EntryID           //entry的全局ID，新增一个entry就加1
	jobWaiter sync.WaitGroup    //run job时会进行add(1)， job 结束会done()，stop整个cron，以此保证所有job都能退出
}

// ScheduleParser is an interface for schedule spec parsers that return a Schedule
//ScheduleParser 是一个接口 用来实现解析调度时间
type ScheduleParser interface {
	Parse(spec string) (Schedule, error)
}

// Job is an interface for submitted cron jobs.
type Job interface { //Job 是一个接口，实现提交的定时任务
	Run()
}

// Schedule describes a job's duty cycle.
type Schedule interface { // Schedule 是Job 的执行周期
	// Next returns the next activation time, later than the given time.
	// Next is invoked initially, and then each time the job is run.
	Next(time.Time) time.Time //Schedule 接口，方法Next返回下次执行的绝对时间，interface可用户自己定制，而Cron自己也有多种时间格式，需要通过Next统一到绝对时间
}

//EntryID是在一个cron 实例中entry的唯一标识
type EntryID int

// Entry consists of a schedule and the func to execute on that schedule.
type Entry struct {
	ID EntryID // 唯一id，用于查询和删除

	// Schedule on which this job should be run.
	Schedule Schedule // 本Entry的调度时间，不是绝对时间

	// Next time the job will run, or the zero time if Cron has not been
	// started or this entry's schedule is unsatisfiable
	Next time.Time // 本entry下次需要执行的绝对时间，会一直被更新

	// Prev is the last time this job was run, or the zero time if never.
	Prev time.Time // 上一次被执行时间，主要用来查询

	// WrappedJob is the thing to run when the Schedule is activated.
	// WrappedJob 是真实执行的Job实体
	// 被封装的含义是Job可以多层嵌套，可以实现基于需要执行Job的额外处理
	// 比如抓取Job异常、如果Job没有返回下一个时间点的Job是还是继续执行还是delay
	WrappedJob Job

	// Job is the thing that was submitted to cron.
	// It is kept around so that user code that needs to get at the job later,
	// e.g. via Entries() can do so.
	// Job 主要给用户查询
	Job Job
}

// Valid returns true if this is not the zero entry.
func (e Entry) Valid() bool { return e.ID != 0 } //entry 是否有效

// byTime is a wrapper for sorting the entry array by time
// (with zero time at the end).
// byTime 用来做sort.Sort()入参
type byTime []*Entry

func (s byTime) Len() int      { return len(s) }
func (s byTime) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byTime) Less(i, j int) bool { //根据下一次将要执行的时间 从小到大排序
	// Two zero times should return false.
	// Otherwise, zero is "greater" than any other time.
	// (To sort it at the end of the list.)
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}

// New returns a new Cron job runner, modified by the given options.
//
// Available Settings
//
//   Time Zone
//     Description: The time zone in which schedules are interpreted
//     Default:     time.Local
//
//   Parser
//     Description: Parser converts cron spec strings into cron.Schedules.
//     Default:     Accepts this spec: https://en.wikipedia.org/wiki/Cron
//
//   Chain
//     Description: Wrap submitted jobs to customize behavior.
//     Default:     A chain that recovers panics and logs them to stderr.
//
// See "cron.With*" to modify the default behavior.
func New(opts ...Option) *Cron { //创建一个cron 实例
	c := &Cron{
		entries:   nil, //初始化为entry 为nil
		chain:     NewChain(),
		add:       make(chan *Entry),
		stop:      make(chan struct{}),
		snapshot:  make(chan chan []Entry),
		remove:    make(chan EntryID),
		running:   false,
		runningMu: sync.Mutex{},
		logger:    DefaultLogger,
		location:  time.Local,
		parser:    standardParser,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FuncJob is a wrapper that turns a func() into a cron.Job
type FuncJob func()

func (f FuncJob) Run() { f() }

// AddFunc adds a func to the Cron to be run on the given schedule.
// The spec is parsed using the time zone of this Cron instance as the default.
// An opaque ID is returned that can be used to later remove it.

//添加一个函数 作为定时任务，这个函数被包装Job  然后调用AddJob 加入到cron 的Entries 中
func (c *Cron) AddFunc(spec string, cmd func()) (EntryID, error) {
	return c.AddJob(spec, FuncJob(cmd))
}

// AddJob adds a Job to the Cron to be run on the given schedule.
// The spec is parsed using the time zone of this Cron instance as the default.
// An opaque ID is returned that can be used to later remove it.
func (c *Cron) AddJob(spec string, cmd Job) (EntryID, error) {
	// 如果是周期固定的时间，返回是绝对年月日的时间，如果是每月循环，则月是一个自定义的bit，包含所有12个月
	// 如果是每一个时间周期重复，则返回是一个绝对时间区间，比如5分钟等
	// 不同的返回最终都是Schedule的interface，不同的返回都实现Next的方法，供上层统一调用
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return 0, err
	}
	return c.Schedule(schedule, cmd), nil
}

// Schedule adds a Job to the Cron to be run on the given schedule.
// The job is wrapped with the configured Chain.
// 生成entry并注册，注意entry是以你的被调用job为粒度，一个job的多次调用策略（比如skip）体现在WrappedJob
func (c *Cron) Schedule(schedule Schedule, cmd Job) EntryID {
	c.runningMu.Lock() //加锁
	defer c.runningMu.Unlock()
	c.nextID++ //entryID 自增
	entry := &Entry{
		ID:         c.nextID,
		Schedule:   schedule,
		WrappedJob: c.chain.Then(cmd),
		Job:        cmd,
	}
	if !c.running {
		c.entries = append(c.entries, entry)
	} else {
		// 如果已经启动，动态加入entry，则通过channel进行加入
		c.add <- entry
	}
	return entry.ID
}

// Entries returns a snapshot of the cron entries.
func (c *Cron) Entries() []Entry {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		replyChan := make(chan []Entry, 1)
		c.snapshot <- replyChan //嵌套通道 做数据交换  todo 还是调用的  c.entrySnapshot() ？ 为什么？
		return <-replyChan
	}
	return c.entrySnapshot()
}

// Location gets the time zone location
func (c *Cron) Location() *time.Location { //当前cron的时区
	return c.location
}

// Entry returns a snapshot of the given entry, or nil if it couldn't be found.
func (c *Cron) Entry(id EntryID) Entry { //通过ID 获得Entry 实体
	for _, entry := range c.Entries() {
		if id == entry.ID {
			return entry
		}
	}
	return Entry{}
}

// Remove an entry from being run in the future.
func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running { //如果cron 正在运行 通过chan 传递  如果没有运行 直接从Entries 切片中删除
		c.remove <- id
	} else {
		c.removeEntry(id)
	}
}

// Start the cron scheduler in its own goroutine, or no-op if already started.
func (c *Cron) Start() {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		return
	}
	c.running = true
	go c.run() //开启一个定时任务调度 在一个新的线程里
}

// Run the cron scheduler, or no-op if already running.
func (c *Cron) Run() {
	c.runningMu.Lock()
	if c.running {
		c.runningMu.Unlock()
		return
	}
	c.running = true
	c.runningMu.Unlock()
	c.run() //开启定时任务调度在当前线程
}

// run the scheduler.. this is private just due to the need to synchronize
// access to the 'running' state variable.
func (c *Cron) run() {
	c.logger.Info("start")

	// Figure out the next activation times for each entry.
	now := c.now()                    //获得当前时间  用来计算每个entry 的激活时间
	for _, entry := range c.entries { // entry 以cron job为粒度
		// 根据当前时间，拿到下一个调度时间
		// GOOD 此时无论Schedule的实际类型是 ConstantDelaySchedule 或者SpecSchedule，得到的都是当前时间往后的最近一次调度的绝对时间
		entry.Next = entry.Schedule.Next(now)
		c.logger.Info("schedule", "now", now, "entry", entry.ID, "next", entry.Next)
	}

	for {
		// Determine the next entry to run.
		//GOOD 进行排序，只需要拿第一个进行时间的定时处理，也就是定时器只需要一个，这是一个典型定时器的实现
		sort.Sort(byTime(c.entries))

		var timer *time.Timer
		if len(c.entries) == 0 || c.entries[0].Next.IsZero() {
			// If there are no entries yet, just sleep - it still handles new entries
			// and stop requests.
			//如果没有调度任务  休眠它（它仍然能处理新的任务 并且停止新的请求）
			timer = time.NewTimer(100000 * time.Hour)
		} else {
			// 减去now，将时间duration更新到最新
			timer = time.NewTimer(c.entries[0].Next.Sub(now))
		}

		for {
			select {
			// 这几个case，保证了在启动后，c.entries的所有读写操作都在这里进行，就不需要使用到锁
			// 外面的操作只需发channel消息进来
			case now = <-timer.C: //到达定时时间
				now = now.In(c.location)
				c.logger.Info("wake", "now", now)

				// Run every entry whose next time was less than now
				for _, e := range c.entries {
					if e.Next.After(now) || e.Next.IsZero() {
						break
					}
					c.startJob(e.WrappedJob)
					e.Prev = e.Next
					e.Next = e.Schedule.Next(now)
					c.logger.Info("run", "now", now, "entry", e.ID, "next", e.Next)
				}

			case newEntry := <-c.add: //新添加一个任务
				timer.Stop()
				now = c.now()
				newEntry.Next = newEntry.Schedule.Next(now)
				c.entries = append(c.entries, newEntry)
				c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)

			case replyChan := <-c.snapshot: //返回所有的实体，做数据交换 单一执行 不会有并发竞争
				replyChan <- c.entrySnapshot()
				continue

			case <-c.stop: //停止定时任务
				timer.Stop() //计时器停止 ，执行的任务并没有停止
				c.logger.Info("stop")
				return

			case id := <-c.remove: //删除一个定时任务
				timer.Stop()
				now = c.now()
				c.removeEntry(id)
				c.logger.Info("removed", "entry", id)
			}

			break //退出循环  重新创建一个timer
		}
	}
}

// startJob runs the given job in a new goroutine.
func (c *Cron) startJob(j Job) {
	c.jobWaiter.Add(1)
	go func() { //开启一个gorountine 执行job
		defer c.jobWaiter.Done()
		j.Run()
	}()
}

// now returns current time in c location
func (c *Cron) now() time.Time { //获得当前时间
	return time.Now().In(c.location)
}

// Stop stops the cron scheduler if it is running; otherwise it does nothing.
// A context is returned so the caller can wait for running jobs to complete.
func (c *Cron) Stop() context.Context {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running { //如果正在运行，发送一个通知给channel（计时器停止）
		c.stop <- struct{}{}
		c.running = false
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		c.jobWaiter.Wait()
		cancel() //手动取消
	}()
	return ctx
}

// entrySnapshot returns a copy of the current cron entry list.
func (c *Cron) entrySnapshot() []Entry { //返回当前cron entry 列表快照
	var entries = make([]Entry, len(c.entries))
	for i, e := range c.entries {
		entries[i] = *e
	}
	return entries
}

func (c *Cron) removeEntry(id EntryID) {
	var entries []*Entry
	for _, e := range c.entries {
		if e.ID != id {
			entries = append(entries, e)
		}
	}
	c.entries = entries
}
