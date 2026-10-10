-- func_arg: a param_name is followed by an arg_class or a func_type, so a
-- type_function_name followed by anything else (PRECISION, ARRAY, '[', ...)
-- is the argument's type. pg_dump names functions by unnamed argument
-- types, as pgvector's ACL entries do.
GRANT ALL ON FUNCTION extensions.vector_avg(double precision[]) TO postgres WITH GRANT OPTION;
GRANT ALL ON FUNCTION extensions.vector_combine(double precision[], double precision[]) TO postgres WITH GRANT OPTION;
GRANT ALL ON FUNCTION extensions.array_to_vector(double precision[], integer, boolean) TO postgres WITH GRANT OPTION;
GRANT ALL ON FUNCTION f(double precision) TO x;
GRANT ALL ON FUNCTION f(int, double precision) TO x;
REVOKE ALL ON FUNCTION f(double precision) FROM PUBLIC;
GRANT ALL ON PROCEDURE p(double precision) TO x;
GRANT ALL ON ROUTINE f(double precision) TO x;
DROP FUNCTION f(double precision);
DROP ROUTINE f(double precision), g(double precision[]);
ALTER FUNCTION f(double precision) OWNER TO x;
COMMENT ON FUNCTION f(double precision) IS 'c';
COMMENT ON AGGREGATE f(double precision) IS 'c';
COMMENT ON AGGREGATE f(double precision ORDER BY double precision) IS 'c';
CREATE AGGREGATE f(double precision) (sfunc = g, stype = int);
CREATE FUNCTION f(double precision) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(IN double precision) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(VARIADIC double precision[]) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(double precision DEFAULT 1) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(double precision = 1) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(a double precision) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(double double precision) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(double int) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(double) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(precision double precision) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(a precision) RETURNS int LANGUAGE sql AS 'select 1';
ALTER EXTENSION e ADD FUNCTION f(double precision);
SECURITY LABEL ON FUNCTION f(double precision) IS 'x';
CREATE CAST (double precision AS int) WITH FUNCTION f(double precision);
GRANT ALL ON FUNCTION f(int4 ARRAY) TO x;
GRANT ALL ON FUNCTION f(int4 ARRAY[3]) TO x;
GRANT ALL ON FUNCTION f(public.t ARRAY) TO x;
CREATE FUNCTION f(a int4 ARRAY) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(a ARRAY) RETURNS int LANGUAGE sql AS 'select 1';
GRANT ALL ON FUNCTION f(character varying, bit varying, national character varying) TO x;
GRANT ALL ON FUNCTION f(timestamp with time zone, time without time zone, interval) TO x;
CREATE FUNCTION f(a OUT double precision, b INOUT int, c IN OUT int, d VARIADIC int[]) RETURNS record LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(a SETOF int) RETURNS int LANGUAGE sql AS 'select 1';
CREATE FUNCTION f(json json, a b.c, d e.f%TYPE, g h(1)) RETURNS int LANGUAGE sql AS 'select 1';
